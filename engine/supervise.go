package main

// 엔진 지킴이 (supervisor)
//
// MFROAD.app(Dock 에 보이는 맥 앱, app/shell/main.swift)이 엔진을 MFROAD_SUPERVISOR=1 로 켜요.
// 지킴이는 진짜 엔진을 자식으로 켜고, 엔진이 '고장으로' 꺼졌을 때만 기록을 남기고 같은 주소로 다시 켜요(5분에 5번까지).
// 시스템이나 사용자가 보낸 종료 신호(SIGTERM 등)는 언제나 그대로 따라요 — 무시하거나 피해 가지 않아요.
// 앱(부모)이 사라지면 지킴이도 엔진을 끄고 끝나요. 엔진이 정상 종료(종료 · 업데이트 후 다시 시작)하면 지킴이도 끝나요.

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxRestarts   = 5
	restartWindow = 5 * time.Minute
)

func supLog(support, format string, a ...any) {
	p := filepath.Join(support, "launcher.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 256<<10 {
		os.Rename(p, filepath.Join(support, "launcher.prev.log"))
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [지킴이 %d] %s\n", time.Now().Format("01-02 15:04:05"), os.Getpid(), fmt.Sprintf(format, a...))
}

func tailFile(p string, n int) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func runSupervisor(support string) {
	detachSelf()
	self, err := os.Executable()
	if err != nil {
		supLog(support, "엔진 위치를 찾지 못했어요: %v", err)
		os.Exit(1)
	}
	supLog(support, "시작 · 엔진 %s · %s/%s · %s", build, runtime.GOOS, runtime.GOARCH, map[string]string{"native": "Dock 앱(MFROAD.app)이 켬"}[env("VIA")])

	var (
		mu       sync.Mutex
		child    *os.Process
		stopping bool
	)
	stop := func(why string) {
		mu.Lock()
		stopping = true
		c := child
		mu.Unlock()
		supLog(support, "%s → 엔진을 끄고 지킴이도 끝냅니다", why)
		if c != nil {
			c.Signal(syscall.SIGTERM)
		} else {
			os.Exit(0)
		}
	}
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		s := <-sig
		stop(fmt.Sprintf("종료 신호(%v)를 받음", s))
	}()
	// 앱(부모)이 사라지면 함께 끝나요 — 엔진이 혼자 남아 몰래 돌지 않게
	if ppid, err := strconv.Atoi(env("PARENT_PID")); err == nil && ppid > 1 {
		go func() {
			for range time.Tick(2 * time.Second) {
				if os.Getppid() != ppid {
					stop("MFROAD 앱이 사라짐")
					return
				}
			}
		}()
	}

	baseEnv := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "MFROAD_SUPERVISOR=") || strings.HasPrefix(kv, "MFROAD_PORT=") || strings.HasPrefix(kv, "MFROAD_PORT_WAIT=") {
			continue
		}
		baseEnv = append(baseEnv, kv)
	}
	port := env("PORT")
	wait := env("PORT_WAIT")
	var crashes []time.Time

	for {
		out := filepath.Join(support, "engine.out")
		if _, err := os.Stat(out); err == nil {
			os.Rename(out, filepath.Join(support, "engine.prev.out"))
		}
		f, _ := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		cmd := exec.Command(self)
		cmd.Env = append(append([]string{}, baseEnv...), "MFROAD_PORT="+port, "MFROAD_PORT_WAIT="+wait, "MFROAD_CHILD=1")
		cmd.Stdout, cmd.Stderr = f, f
		started := time.Now()
		if err := cmd.Start(); err != nil {
			supLog(support, "엔진을 켜지 못했어요: %v", err)
			if f != nil {
				f.Close()
			}
			os.Exit(1)
		}
		mu.Lock()
		child = cmd.Process
		mu.Unlock()
		supLog(support, "엔진 켬 (pid %d, 포트 %s)", cmd.Process.Pid, orDash(port))
		err := cmd.Wait()
		if f != nil {
			f.Close()
		}
		code := cmd.ProcessState.ExitCode()
		how := fmt.Sprintf("종료 코드 %d", code)
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			how = fmt.Sprintf("신호 %v 로 꺼짐", ws.Signal())
		}
		ran := time.Since(started).Round(time.Second)
		mu.Lock()
		stopped := stopping
		child = nil
		mu.Unlock()
		if stopped {
			supLog(support, "엔진을 껐어요 (%s)", how)
			return
		}
		// 엔진에게만 종료 신호가 간 경우(활동 상태 보기의 종료 등)도 일부러 끈 것으로 보고 다시 켜지 않아요
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() && (ws.Signal() == syscall.SIGTERM || ws.Signal() == syscall.SIGINT || ws.Signal() == syscall.SIGHUP || ws.Signal() == syscall.SIGKILL) {
			supLog(support, "엔진이 종료 신호로 꺼져서 다시 켜지 않아요 (%s)", how)
			return
		} else if err == nil && code == 0 {
			supLog(support, "엔진이 정상적으로 끝났어요 (%s 동안 실행)", ran)
			return
		} else {
			supLog(support, "⚠️ 엔진이 갑자기 꺼졌어요: %s, %s 동안 실행\n----- 엔진 마지막 출력 -----\n%s\n----------------------------", how, ran, headTail(out, 30, 8))
		}

		now := time.Now()
		kept := crashes[:0]
		for _, t := range crashes {
			if now.Sub(t) < restartWindow {
				kept = append(kept, t)
			}
		}
		crashes = append(kept, now)
		if len(crashes) > maxRestarts {
			supLog(support, "5분 안에 %d번 꺼져서 더 이상 다시 켜지 않아요", len(crashes))
			os.Exit(1)
		}
		// 같은 포트·토큰으로 다시 켜서 열려 있던 창이 그대로 이어지게 해요
		if b, err := os.ReadFile(filepath.Join(support, "lastport")); err == nil && strings.TrimSpace(string(b)) != "" {
			port = strings.TrimSpace(string(b))
		}
		wait = "1"
		time.Sleep(700 * time.Millisecond)
	}
}

func orDash(s string) string {
	if s == "" {
		return "자동"
	}
	return s
}

// headTail keeps the first lines (where Go prints "panic:" / "fatal error:") and the last few.
func headTail(p string, head, tail int) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) <= head+tail+1 {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:head], "\n") + fmt.Sprintf("\n… (%d줄 생략) …\n", len(lines)-head-tail) + strings.Join(lines[len(lines)-tail:], "\n")
}
