package main

// 진단 정보
// - 최근 요청 기록 (경로 · 결과 · 걸린 시간 · 브라우저)
// - 화면에서 생긴 오류 (/api/clientlog 로 받음)
// - 살아 있다는 신호(alive)와 정상 종료 표시(clean-exit): 다음 실행 때 지난번이 어떻게 끝났는지 알 수 있어요
// - /diag : 토큰 없이 볼 수 있는 읽기 전용 상태 페이지 (127.0.0.1 에서만, 다른 사이트는 읽을 수 없음)
//           토큰 · 나무증권 키 · 보유 내역은 담지 않아요.

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type reqRec struct {
	Method string
	Path   string
	Status int
	Ms     int64
	Note   string
}

type diagState struct {
	mu        sync.Mutex
	agents    map[string]int
	prevEnd   string // 지난번 엔진이 어떻게 끝났는지
	startedAt time.Time
}

var diag = &diagState{agents: map[string]int{}, startedAt: time.Now()}

// 기록은 파일(requests.log)에도 남겨서, 엔진이 갑자기 꺼져도 그 직전 상황을 볼 수 있어요.
func (d *diagState) line(s string) {
	if app == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := filepath.Join(app.support, "requests.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 512<<10 {
		os.Rename(p, filepath.Join(app.support, "requests.prev.log"))
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s [%d] %s\n", time.Now().Format("01-02 15:04:05"), os.Getpid(), s)
		f.Close()
	}
}

func (d *diagState) add(r reqRec) {
	d.line(fmt.Sprintf("%-4s %-22s %3d %5dms %s", r.Method, r.Path, r.Status, r.Ms, r.Note))
}

func (d *diagState) agent(ua string) {
	d.mu.Lock()
	first := d.agents[ua] == 0
	d.agents[ua]++
	d.mu.Unlock()
	if first {
		d.line("화면 열림 · " + ua)
	}
}

func (d *diagState) clientErr(s string) { d.line("화면 문제 · " + s) }

// statusWriter remembers the status code a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = 200
	}
	return s.ResponseWriter.Write(b)
}

// recordRequests wraps the whole server: every request is noted (thumbnails and files only when they fail).
func recordRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		t0 := time.Now()
		defer func() {
			v := recover()
			note := ""
			if v != nil {
				note = fmt.Sprintf("PANIC %v", v)
				sw.code = 599
			}
			p := r.URL.Path
			quiet := (p == "/api/poll" || p == "/api/search" || p == "/api/quote" || strings.HasPrefix(p, "/fonts/")) && sw.code < 400 && v == nil
			if !quiet {
				diag.add(reqRec{Method: r.Method, Path: p, Status: sw.code, Ms: time.Since(t0).Milliseconds(), Note: note})
			}
			if v != nil {
				panic(v) // 원래처럼 처리되게 다시 던져요 (api 래퍼가 이미 잡지 못한 경우)
			}
		}()
		if r.URL.Path == "/" {
			diag.agent(r.Header.Get("User-Agent"))
		}
		next.ServeHTTP(sw, r)
	})
}

// ---------------------------------------------------------------- 살아 있음 · 정상 종료 표시

func heartbeat(support string) {
	alive := filepath.Join(support, "alive")
	clean := filepath.Join(support, "clean-exit")
	// 지난번이 어떻게 끝났는지
	if b, err := os.ReadFile(alive); err == nil {
		last := strings.TrimSpace(string(b))
		if c, err := os.ReadFile(clean); err == nil {
			diag.prevEnd = "정상 종료 (" + strings.TrimSpace(string(c)) + ")"
		} else {
			diag.prevEnd = "⚠️ 기록 없이 사라짐 — 마지막 신호 " + last
		}
	} else {
		diag.prevEnd = "기록 없음 (처음 실행)"
	}
	os.Remove(clean)
	for {
		writeFileAtomic(alive, []byte(time.Now().Format("01-02 15:04:05")+" pid "+strconv.Itoa(os.Getpid())), 0o644)
		time.Sleep(5 * time.Second)
	}
}

func markCleanExit(support, why string) {
	writeFileAtomic(filepath.Join(support, "clean-exit"), []byte(time.Now().Format("01-02 15:04:05")+" "+why), 0o644)
}

// ---------------------------------------------------------------- /diag

var osVersion = sync.OnceValue(func() string {
	if runtime.GOOS != "darwin" {
		return runtime.GOOS
	}
	out, err := exec.Command("/usr/bin/sw_vers").Output()
	if err != nil {
		return "macOS (버전 확인 실패)"
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(string(out), "\n", " · ")), " ")
})

func (a *App) handleDiag(w http.ResponseWriter, r *http.Request) {
	s := a.Settings()
	var b strings.Builder
	p := func(format string, x ...any) { fmt.Fprintf(&b, format+"\n", x...) }
	p("MFROAD 진단 정보 — %s", time.Now().Format("2006-01-02 15:04:05"))
	p("")
	p("엔진       v%s · pid %d · 켜진 지 %s · 포트 %d", build, os.Getpid(), time.Since(diag.startedAt).Round(time.Second), a.port)
	p("지킴이     %s", map[bool]string{true: "있음", false: "없음"}[env("CHILD") == "1"])
	p("시스템     %s · %s · CPU %d개", osVersion(), runtime.GOARCH, runtime.NumCPU())
	p("지난번     %s", diag.prevEnd)
	p("앱 위치    %s", env("APP"))
	p("설정       화면=%s · 시세갱신=%s · 자동업데이트=%v · 계좌자동불러오기=%v", s.Appearance, s.Refresh, s.AutoUpdate, s.AutoImport)
	pf := a.pf.Snapshot()
	p("자산       계좌 %d개 · 기록 %d개 · 시세 %d개", len(pf.Accounts), len(pf.Lots), a.mk.Count())
	p("나무증권   %s", a.nh.DiagLine())
	p("업비트     %s", a.mk.CoinDiag())
	p("종목목록   %s", a.master.DiagLine())
	u := a.upd.State()
	p("업데이트   %v · 최신 %v · %v", u["status"], latestVer(u["latest"]), u["error"])

	p("")
	p("── 실행기 · 지킴이 기록 ──")
	p("%s", tailFile(filepath.Join(a.support, "launcher.log"), 150))
	p("")
	p("── 요청 · 화면 기록 (엔진이 꺼지기 직전 것도 남아 있어요) ──")
	p("%s", tailFile(filepath.Join(a.support, "requests.log"), 150))
	if t := headTail(filepath.Join(a.support, "engine.prev.out"), 40, 15); t != "" {
		p("")
		p("── 지난번 엔진 출력 ──")
		p("%s", t)
	}
	if t := tailFile(filepath.Join(a.support, "engine.prev.log"), 40); t != "" {
		p("")
		p("── 지난번 엔진 기록 ──")
		p("%s", t)
	}
	p("")
	p("── 이번 엔진 기록 ──")
	p("%s", tailFile(filepath.Join(a.support, "engine.log"), 80))

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write([]byte(b.String()))
}

func latestVer(v any) string {
	if r, ok := v.(*Release); ok && r != nil {
		return r.Version
	}
	return "—"
}
