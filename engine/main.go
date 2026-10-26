// MFROAD — 국내주식 · 코인 자산 확인 (macOS, Apple Silicon)
// made by. Nevertheless_D
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// build is the app version, set with -ldflags "-X main.build=1.2.3".
var build = "0.0.0-dev"

//go:embed web
var webFiles embed.FS

type Settings struct {
	Appearance string         `json:"appearance"` // system | light | dark
	Refresh    string         `json:"refresh"`    // live | 5s | 30s | 60s
	AutoUpdate bool           `json:"autoUpdate"`
	AutoImport bool           `json:"autoImport"` // 나무증권 계좌에서 보유 종목 자동으로 불러오기
	UI         map[string]any `json:"ui"`
}

type App struct {
	support string
	token   string
	port    int

	mu       sync.Mutex
	settings Settings

	pf     *Portfolio
	mk     *Market
	nh     *NH
	master *Master
	coins  *Upbit
	hist   *History
	upd    *Updater

	lastSeen atomic.Int64
}

var app *App

// started 는 단조 시계 기준점이에요 (잠자는 동안은 늘어나지 않아요).
var started = time.Now()

func touch() { app.lastSeen.Store(int64(time.Since(started))) }

func env(k string) string { return os.Getenv("MFROAD_" + k) }

func main() {
	log.SetFlags(log.LstdFlags)
	home, _ := os.UserHomeDir()
	support := env("SUPPORT")
	if support == "" {
		if runtime.GOOS == "darwin" {
			support = filepath.Join(home, "Library", "Application Support", "MFROAD")
		} else {
			support = filepath.Join(home, ".mfroad")
		}
	}
	os.MkdirAll(support, 0o755)
	if env("SUPERVISOR") == "1" {
		runSupervisor(support)
		return
	}
	token := env("TOKEN")
	if token == "" {
		token = randID(16)
	}
	app = &App{support: support, token: token}
	os.Rename(filepath.Join(support, "engine.log"), filepath.Join(support, "engine.prev.log"))
	if f, err := os.OpenFile(filepath.Join(support, "engine.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	loadTestOverrides(support)
	app.loadSettings()
	touch()
	app.pf = NewPortfolio(support)
	app.master = NewMaster(support)
	app.coins = NewUpbit(support)
	app.nh = NewNH(support)
	app.mk = NewMarket()
	app.hist = NewHistory(support)
	app.upd = NewUpdater(support)

	go heartbeat(support)
	ln := listen(env("PORT"), env("PORT_WAIT") == "1")
	app.port = ln.Addr().(*net.TCPAddr).Port
	writeFileAtomic(filepath.Join(support, "token"), []byte(token), 0o600)
	writeFileAtomic(filepath.Join(support, "port"), []byte(strconv.Itoa(app.port)), 0o644)
	// lastport 는 지우지 않아요. 엔진이 다시 켜지면 같은 주소로 켜져서, 열려 있던 창이 그대로 이어져요.
	writeFileAtomic(filepath.Join(support, "lastport"), []byte(strconv.Itoa(app.port)), 0o644)
	log.Printf("MFROAD %s listening on %d (pid %d, 지킴이 %v)", build, app.port, os.Getpid(), env("CHILD") == "1")

	if app.port != defaultPort {
		go func() {
			defer recoverLog("diag listener")
			if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort)); err == nil {
				m := http.NewServeMux()
				m.HandleFunc("/diag", app.handleDiag)
				http.Serve(ln, hostGuard(m))
			}
		}()
	}
	go app.watchParent()
	go func() { defer recoverLog("background"); app.startBackground() }()
	go func() { defer recoverLog("autoLoop"); app.upd.autoLoop() }()
	srv := &http.Server{Handler: hostGuard(recordRequests(app.routes())), ErrorLog: log.Default()}
	log.Fatal(srv.Serve(ln))
}

// defaultPort: 늘 같은 주소를 쓰면 엔진이 다시 켜져도 열려 있던 창이 이어지고, 진단 페이지도 찾기 쉬워요.
const defaultPort = 47931

func listen(pref string, wait bool) net.Listener {
	p, err := strconv.Atoi(pref)
	if err != nil || p <= 0 {
		p = defaultPort
	}
	tries := 1
	if wait { // 다시 시작할 때는 이전 엔진이 주소를 놓을 때까지 잠깐 기다려요
		tries = 20
	}
	for i := 0; i < tries; i++ {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			return ln
		}
		time.Sleep(150 * time.Millisecond)
	}
	if p != defaultPort {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort)); err == nil {
			return ln
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	return ln
}

// startBackground: 키체인에서 나무증권 키를 읽고, 종목 목록을 준비하고, 시세 · 자동 불러오기 · 기록 반복을 켜요.
func (a *App) startBackground() {
	a.nh.LoadKeys()
	go func() { defer recoverLog("master"); a.master.Ensure() }()
	go func() { defer recoverLog("coin list"); a.coins.EnsureMarkets() }()
	go func() { defer recoverLog("coinLoop"); a.coinLoop() }()
	go func() { defer recoverLog("stockLoop"); a.stockLoop() }()
	go func() { defer recoverLog("importLoop"); a.importLoop() }()
	go func() { defer recoverLog("historyLoop"); a.historyLoop() }()
}

// ---------------------------------------------------------------- lifecycle

func (a *App) shutdown() { a.shutdownWhy("종료") }

// nativeShell: Dock 에 보이는 맥 앱(app/shell/main.swift)이 엔진을 켰는지
func nativeShell() bool { return env("SHELL") == "native" }

// watchParent: 지킴이(부모)가 사라지면 엔진도 끝나요 — 혼자 남아 몰래 돌지 않게
func (a *App) watchParent() {
	if env("CHILD") != "1" {
		return
	}
	ppid := os.Getppid()
	for range time.Tick(2 * time.Second) {
		if os.Getppid() != ppid {
			a.shutdownWhy("지킴이가 사라짐")
		}
	}
}

func (a *App) shutdownWhy(why string) {
	log.Printf("shutdown: %s", why)
	markCleanExit(a.support, why)
	if b, err := os.ReadFile(filepath.Join(a.support, "port")); err == nil && string(b) == strconv.Itoa(a.port) {
		os.Remove(filepath.Join(a.support, "port"))
	}
	os.Exit(0)
}

// ---------------------------------------------------------------- settings

func (a *App) settingsPath() string { return filepath.Join(a.support, "settings.json") }

func (a *App) loadSettings() {
	a.settings = Settings{Appearance: "system", Refresh: "live", AutoUpdate: true, AutoImport: true, UI: map[string]any{}}
	if b, err := os.ReadFile(a.settingsPath()); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	if a.settings.UI == nil {
		a.settings.UI = map[string]any{}
	}
	switch a.settings.Refresh {
	case "live", "5s", "30s", "60s":
	default:
		a.settings.Refresh = "live"
	}
}

func (a *App) saveSettings() {
	b, _ := json.MarshalIndent(a.settings, "", "  ")
	writeFileAtomic(a.settingsPath(), b, 0o644)
}

func (a *App) Settings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	s.UI = make(map[string]any, len(a.settings.UI))
	for k, v := range a.settings.UI {
		s.UI[k] = v
	}
	return s
}

func (a *App) settingsJSON() map[string]any {
	s := a.Settings()
	return map[string]any{
		"appearance": s.Appearance,
		"refresh":    s.Refresh,
		"autoUpdate": s.AutoUpdate,
		"autoImport": s.AutoImport,
		"ui":         s.UI,
		"build":      build,
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	importNow := false
	if r.Method == http.MethodPost {
		var in map[string]json.RawMessage
		if err := readJSON(r, &in); err != nil {
			jsonError(w, 400, "bad request")
			return
		}
		a.mu.Lock()
		for k, v := range in {
			switch k {
			case "appearance":
				var s string
				if json.Unmarshal(v, &s) == nil && (s == "system" || s == "light" || s == "dark") {
					a.settings.Appearance = s
				}
			case "refresh":
				var s string
				if json.Unmarshal(v, &s) == nil && (s == "live" || s == "5s" || s == "30s" || s == "60s") {
					a.settings.Refresh = s
				}
			case "autoUpdate":
				json.Unmarshal(v, &a.settings.AutoUpdate)
			case "autoImport":
				prev := a.settings.AutoImport
				json.Unmarshal(v, &a.settings.AutoImport)
				importNow = !prev && a.settings.AutoImport
			case "ui":
				var ui map[string]any
				if json.Unmarshal(v, &ui) == nil {
					for kk, vv := range ui {
						a.settings.UI[kk] = vv
					}
				}
			}
		}
		a.saveSettings()
		a.mu.Unlock()
		a.mk.Wake()
		if importNow {
			go func() { defer recoverLog("import"); a.importNH() }()
		}
	}
	writeJSON(w, a.settingsJSON())
}

// ---------------------------------------------------------------- routes

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(sub))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if r.URL.Query().Get("t") != a.token {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, "<meta charset=utf-8><body style='font:15px -apple-system;padding:40px'>MFROAD 앱을 다시 실행해 주세요.</body>")
				return
			}
			b, _ := webFiles.ReadFile("web/index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(r.URL.Path, "/fonts/") {
			w.Header().Set("Cache-Control", "public, max-age=604800")
		}
		static.ServeHTTP(w, r)
	})

	mux.HandleFunc("/diag", a.handleDiag)

	api := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			t := r.Header.Get("x-token")
			if t == "" {
				t = r.URL.Query().Get("t")
			}
			if t != a.token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			touch()
			defer func() {
				if v := recover(); v != nil {
					log.Printf("PANIC in %s: %v\n%s", path, v, debug.Stack())
					jsonError(w, 500, "앱 안에서 문제가 생겼어요. 다시 시도해 주세요")
				}
			}()
			h(w, r)
		})
	}

	api("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "build": build, "shell": env("SHELL")})
	})
	api("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
		go func() { time.Sleep(300 * time.Millisecond); a.shutdownWhy("종료 버튼") }()
	})
	api("/api/clientlog", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Items []string `json:"items"`
		}
		readJSON(r, &in)
		for i, x := range in.Items {
			if i >= 30 {
				break
			}
			if len(x) > 600 {
				x = x[:600]
			}
			diag.clientErr(x)
			log.Printf("client: %s", x)
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/crash", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "report": crashReport(a.support)})
	})
	api("/api/poll", a.handlePoll)
	api("/api/history", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "today": a.hist.Today(400)})
	})
	api("/api/settings", a.handleSettings)

	// 자산
	api("/api/portfolio", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.pf.Snapshot()) })
	api("/api/lot/save", a.handleLotSave)
	api("/api/lot/delete", a.handleLotDelete)
	api("/api/account/save", a.handleAccountSave)
	api("/api/account/delete", a.handleAccountDelete)
	api("/api/search", a.handleSearch)
	api("/api/quote", a.handleQuote)

	// 나무증권
	api("/api/nh/keys", a.handleNHKeys)
	api("/api/nh/forget", a.handleNHForget)
	api("/api/nh/test", a.handleNHTest)
	api("/api/nh/sync", a.handleNHSync)

	// 업데이트
	api("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		a.upd.Check(true)
		writeJSON(w, a.upd.State())
	})
	api("/api/update/install", func(w http.ResponseWriter, r *http.Request) {
		go func() { defer recoverLog("install"); a.upd.Install() }()
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/update/rollback", func(w http.ResponseWriter, r *http.Request) {
		if err := a.upd.Rollback(); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/update/restart", func(w http.ResponseWriter, r *http.Request) {
		if err := a.upd.Restart(); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	return mux
}

// Only answer requests addressed to 127.0.0.1 / localhost (DNS rebinding protection).
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- utils

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func randID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func init() {
	mime.AddExtensionType(".js", "text/javascript")
	mime.AddExtensionType(".woff2", "font/woff2")
}

func recoverLog(where string) {
	if v := recover(); v != nil {
		log.Printf("PANIC in %s: %v\n%s", where, v, debug.Stack())
	}
}

// crashReport: 지난번 엔진이 갑자기 멈췄다면 그 기록(engine.prev.out)과 이번 기록의 문제 부분을 돌려줘요.
func crashReport(support string) string {
	var parts []string
	if b, err := os.ReadFile(filepath.Join(support, "engine.prev.out")); err == nil {
		t := string(b)
		if strings.Contains(t, "panic") || strings.Contains(t, "fatal error") || strings.Contains(t, "PANIC") {
			if len(t) > 6000 {
				t = t[len(t)-6000:]
			}
			parts = append(parts, "[지난번 실행]\n"+t)
		}
	}
	if b, err := os.ReadFile(filepath.Join(support, "engine.log")); err == nil {
		t := string(b)
		if i := strings.Index(t, "PANIC"); i >= 0 {
			t = t[i:]
			if len(t) > 6000 {
				t = t[:6000]
			}
			parts = append(parts, "[이번 실행]\n"+t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// num: 응답 값이 숫자이든 글자("72,300", " 1.5 ")이든 숫자로 읽어요.
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(x, ",", ""))
		s = strings.TrimPrefix(s, "+")
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return 0
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
