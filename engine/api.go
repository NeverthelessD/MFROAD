package main

// 화면에서 쓰는 검색 · 시세 · 나무증권 연결 API

import (
	"net/http"
	"strings"
	"time"
)

// GET /api/search?kind=stock|coin&q=삼성
func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 40 {
		q = string([]rune(q)[:40])
	}
	type item struct {
		Kind   string  `json:"kind"`
		Code   string  `json:"code"`
		Name   string  `json:"name"`
		Market string  `json:"market"`
		Price  float64 `json:"price"`
		Rate   float64 `json:"rate"`
		Source string  `json:"source"`
	}
	out := []item{}
	if kind == "coin" {
		a.coins.WaitReady(12 * time.Second)
		list, err := a.coins.Search(q, 12)
		if err != nil {
			jsonError(w, 503, err.Error())
			return
		}
		var codes []string
		for _, c := range list {
			codes = append(codes, c.Code)
		}
		// 찾은 코인의 지금 값을 한 번에 받아 와요 (업비트는 한 번에 여러 개를 줘요)
		if len(codes) > 0 {
			if qs, err := a.coins.Tickers(codes); err == nil {
				for _, x := range qs {
					a.mk.Put(x)
				}
			}
		}
		for _, c := range list {
			it := item{Kind: "coin", Code: c.Code, Name: c.Name, Market: "KRW"}
			if x := a.mk.Get("coin", c.Code); x != nil {
				it.Price, it.Rate, it.Source = x.Price, x.Rate, x.Source
			}
			out = append(out, it)
		}
	} else {
		a.master.WaitReady(12 * time.Second)
		list, err := a.master.Search(q, 12)
		if err != nil {
			jsonError(w, 503, err.Error())
			return
		}
		for _, s := range list {
			it := item{Kind: "stock", Code: s.Code, Name: s.Name, Market: s.Market, Price: s.Prev, Source: "close"}
			if x := a.mk.Get("stock", s.Code); x != nil && x.Source == "nh" {
				it.Price, it.Rate, it.Source = x.Price, x.Rate, x.Source
			}
			out = append(out, it)
		}
	}
	writeJSON(w, map[string]any{"ok": true, "items": out})
}

// GET /api/quote?kind=&code= — 지금 값을 바로 물어봐요 (종목 추가 창에서 고른 종목)
func (a *App) handleQuote(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if code == "" || len(code) > 20 {
		jsonError(w, 400, "종목을 골라 주세요")
		return
	}
	if kind == "coin" {
		qs, err := a.coins.Tickers([]string{code})
		if err != nil {
			jsonError(w, 503, err.Error())
			return
		}
		for _, x := range qs {
			a.mk.Put(x)
		}
	} else {
		cached := a.mk.Get("stock", code)
		fresh := cached != nil && cached.Source == "nh" && time.Since(time.UnixMilli(cached.At)) < 5*time.Second
		if a.nh.Configured() && !fresh {
			if x, err := a.nh.Price(code); err == nil {
				if s := a.master.Lookup(code); s != nil && x.Name == "" {
					x.Name = s.Name
				}
				a.mk.Put(x)
			}
		}
		if a.mk.Get("stock", code) == nil {
			if s := a.master.Lookup(code); s != nil && s.Prev > 0 {
				a.mk.Put(&Quote{Kind: "stock", Code: code, Name: s.Name, Price: s.Prev, Prev: s.Prev, Source: "close", At: a.master.LoadedAt()})
			}
		}
	}
	q := a.mk.Get(kind, code)
	if q == nil {
		jsonError(w, 404, "지금 값을 받지 못했어요")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "quote": q})
}

// POST /api/nh/keys {appKey, appSecret, brand} — 키로 토큰을 받아 보고, 되면 키체인에 저장해요
func (a *App) handleNHKeys(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AppKey    string `json:"appKey"`
		AppSecret string `json:"appSecret"`
		Brand     string `json:"brand"`
	}
	if err := readJSON(r, &in); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if err := a.nh.SaveKeys(in.AppKey, in.AppSecret, in.Brand); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	in.AppSecret = ""
	accts, err := a.nh.Accounts()
	res := map[string]any{"ok": true, "nh": a.nh.Status(), "accounts": len(accts)}
	if err != nil {
		res["warning"] = err.Error()
	}
	if a.Settings().AutoImport {
		go func() { defer recoverLog("import"); a.importNH() }()
	}
	a.mk.Wake()
	writeJSON(w, res)
}

func (a *App) handleNHForget(w http.ResponseWriter, r *http.Request) {
	a.nh.Forget()
	writeJSON(w, map[string]any{"ok": true, "nh": a.nh.Status()})
}

// POST /api/nh/test — 연결 확인 (계좌 목록을 물어봐요. 토큰은 저장해 둔 것을 써요)
func (a *App) handleNHTest(w http.ResponseWriter, r *http.Request) {
	if !a.nh.Configured() {
		jsonError(w, 400, "나무증권 키를 먼저 넣어 주세요")
		return
	}
	accts, err := a.nh.Accounts()
	if err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "accounts": len(accts), "nh": a.nh.Status()})
}

// POST /api/nh/sync — 지금 계좌에서 다시 불러오기
func (a *App) handleNHSync(w http.ResponseWriter, r *http.Request) {
	if !a.nh.Configured() {
		jsonError(w, 400, "나무증권 키를 먼저 넣어 주세요")
		return
	}
	n, err := a.importNH()
	if err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "imported": n, "rev": a.pf.Rev(), "nh": a.nh.Status()})
}
