package main

// 시세 모음 · 반복해서 가져오기
//
//   코인  업비트 /v1/ticker 한 번에 여러 개 — 실시간이면 1초마다
//   주식  나무증권 연결 → 보유 종목 현재가를 돌아가며(초당 4회 안에서), 연결 안 됨 → 종목 목록의 전일 종가
//         장이 닫힌 시간(평일 20:10~07:50 · 주말)에는 5분마다만
//   자동 불러오기  나무증권 계좌 잔고 → 그 계좌의 기록을 통째로 바꿈 (켤 때 · 10분마다 · 버튼)
//   기록  1분마다 총자산을 history 에 남겨 오늘 그래프를 그려요

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Quote struct {
	Kind   string  `json:"kind"`
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
	Change float64 `json:"change"` // 전일 대비 (부호 포함)
	Rate   float64 `json:"rate"`   // 전일 대비 % (부호 포함)
	Prev   float64 `json:"prev"`
	Source string  `json:"source"` // nh | close | upbit
	At     int64   `json:"at"`
}

func qkey(kind, code string) string { return kind + ":" + code }

type Market struct {
	mu      sync.Mutex
	quotes  map[string]*Quote
	rev     int64
	wake    chan struct{}
	coinErr string
	coinAt  int64
	stockAt int64
	stockSrc string
}

func NewMarket() *Market {
	return &Market{quotes: map[string]*Quote{}, wake: make(chan struct{}, 4), rev: 1}
}

func (m *Market) Wake() {
	for i := 0; i < 2; i++ {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

func (m *Market) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.quotes)
}

func (m *Market) Put(q *Quote) {
	if q == nil || q.Price <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := qkey(q.Kind, q.Code)
	old := m.quotes[k]
	if old != nil && q.Name == "" {
		q.Name = old.Name
	}
	// 더 정확한 시세(나무증권)를 전일 종가로 덮지 않아요
	if old != nil && old.Source == "nh" && q.Source == "close" && time.Since(time.UnixMilli(old.At)) < 6*time.Hour {
		return
	}
	if old == nil || old.Price != q.Price || old.Rate != q.Rate || old.Source != q.Source {
		m.rev++
	}
	m.quotes[k] = q
	if q.Kind == "coin" {
		m.coinAt = q.At
	} else {
		m.stockAt = q.At
		m.stockSrc = q.Source
	}
}

func (m *Market) Get(kind, code string) *Quote {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q := m.quotes[qkey(kind, code)]; q != nil {
		c := *q
		return &c
	}
	return nil
}

func (m *Market) Snapshot(keys map[string]bool) (map[string]*Quote, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]*Quote{}
	for k, q := range m.quotes {
		if keys == nil || keys[k] {
			c := *q
			out[k] = &c
		}
	}
	return out, m.rev
}

func (m *Market) Rev() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rev
}

func (m *Market) setCoinErr(e string) {
	m.mu.Lock()
	if m.coinErr != e {
		m.rev++
	}
	m.coinErr = e
	m.mu.Unlock()
}

func (m *Market) CoinDiag() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.coinErr != "" {
		return "⚠️ " + m.coinErr
	}
	if m.coinAt == 0 {
		return "아직 받은 시세 없음"
	}
	return "마지막 시세 " + time.UnixMilli(m.coinAt).Format("15:04:05")
}

func (m *Market) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{"coinError": m.coinErr, "coinAt": m.coinAt, "stockAt": m.stockAt, "stockSource": m.stockSrc}
}

// ---------------------------------------------------------------- 간격

var kst = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Seoul"); err == nil {
		return l
	}
	return time.FixedZone("KST", 9*3600)
}()

// stockMarketOpen: 국내 주식 시장(정규장 + NXT 프리·애프터마켓)이 열려 있을 수 있는 시간
func stockMarketOpen(t time.Time) bool {
	k := t.In(kst)
	if k.Weekday() == time.Saturday || k.Weekday() == time.Sunday {
		return false
	}
	mins := k.Hour()*60 + k.Minute()
	return mins >= 7*60+50 && mins <= 20*60+10
}

func refreshEvery(s string) time.Duration {
	switch s {
	case "5s":
		return 5 * time.Second
	case "30s":
		return 30 * time.Second
	case "60s":
		return time.Minute
	}
	return time.Second
}

// sleepOrWake: 정해진 만큼 쉬되, 설정이 바뀌거나 종목이 추가되면 바로 깨요
func (a *App) sleepOrWake(d time.Duration) {
	select {
	case <-time.After(d):
	case <-a.mk.wake:
	}
}

// ---------------------------------------------------------------- 코인

func (a *App) coinLoop() {
	for {
		codes := a.pf.Held("coin")
		if len(codes) > 0 {
			qs, err := a.coins.Tickers(codes)
			if err != nil {
				a.mk.setCoinErr(err.Error())
			} else {
				a.mk.setCoinErr("")
				for _, q := range qs {
					a.mk.Put(q)
				}
			}
		}
		d := refreshEvery(a.Settings().Refresh)
		if len(codes) == 0 {
			d = 30 * time.Second
		}
		a.sleepOrWake(d)
	}
}

// ---------------------------------------------------------------- 주식

func (a *App) stockLoop() {
	idx := 0
	for {
		codes := a.pf.Held("stock")
		if len(codes) == 0 {
			a.sleepOrWake(30 * time.Second)
			continue
		}
		// 나무증권 시세가 아직 없는 종목은 전일 종가로 먼저 채워요
		for _, c := range codes {
			if q := a.mk.Get("stock", c); q == nil || q.Source == "close" {
				if s := a.master.Lookup(c); s != nil && s.Prev > 0 {
					a.mk.Put(&Quote{Kind: "stock", Code: c, Name: s.Name, Price: s.Prev, Prev: s.Prev, Source: "close", At: a.master.LoadedAt()})
				}
			}
		}
		if !a.nh.Configured() {
			a.sleepOrWake(30 * time.Second)
			continue
		}
		every := refreshEvery(a.Settings().Refresh)
		open := stockMarketOpen(time.Now())
		// 한 바퀴: 보유 종목을 하나씩 (호출 간격은 nh.throttle 이 지켜요)
		start := time.Now()
		failed := 0
		for i := 0; i < len(codes); i++ {
			c := codes[(idx+i)%len(codes)]
			if !open {
				// 장이 닫혀 있으면 이미 받은 시세는 다시 묻지 않아요
				if q := a.mk.Get("stock", c); q != nil && q.Source == "nh" && time.Since(time.UnixMilli(q.At)) < 3*time.Hour {
					continue
				}
			}
			q, err := a.nh.Price(c)
			if err != nil {
				failed++
				if strings.Contains(err.Error(), "너무 자주") {
					time.Sleep(1500 * time.Millisecond)
				}
				if failed >= 3 && i == failed-1 { // 처음부터 계속 실패하면 이번 바퀴는 쉬어요
					break
				}
				continue
			}
			if s := a.master.Lookup(c); s != nil && q.Name == "" {
				q.Name = s.Name
			}
			a.mk.Put(q)
		}
		idx++
		d := every
		if !open {
			d = 5 * time.Minute
		} else if failed > 0 && failed == len(codes) {
			d = 20 * time.Second
		}
		if el := time.Since(start); el < d {
			a.sleepOrWake(d - el)
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------- 나무증권 자동 불러오기

func (a *App) importLoop() {
	time.Sleep(3 * time.Second)
	for {
		if a.nh.Configured() && a.Settings().AutoImport {
			a.importNH()
		}
		time.Sleep(10 * time.Minute)
	}
}

var importMu sync.Mutex

// importNH: 나무증권의 모든 운영 계좌 잔고를 불러와 그 계좌의 기록을 바꿔요.
func (a *App) importNH() (int, error) {
	importMu.Lock()
	defer importMu.Unlock()
	n := a.nh
	n.mu.Lock()
	n.syncing = true
	n.mu.Unlock()
	count, err := a.doImport()
	n.mu.Lock()
	n.syncing = false
	n.lastSync = time.Now()
	n.syncN = count
	if err != nil {
		n.syncErr = err.Error()
	} else {
		n.syncErr = ""
	}
	n.mu.Unlock()
	a.mk.Wake()
	return count, err
}

func (a *App) doImport() (int, error) {
	accts, err := a.nh.Accounts()
	if err != nil {
		return 0, err
	}
	if len(accts) == 0 {
		return 0, fmt.Errorf("나무증권에서 주식 계좌를 찾지 못했어요")
	}
	total := 0
	var errs []string
	for _, ac := range accts {
		hs, err := a.nh.Balance(ac.No)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var lots []Lot
		for _, h := range hs {
			name, mk := h.Name, ""
			if s := a.master.Lookup(h.Code); s != nil {
				name, mk = s.Name, s.Market
			}
			lots = append(lots, Lot{Kind: "stock", Code: h.Code, Name: name, Market: mk, Qty: h.Qty, Avg: h.Avg})
			if h.Price > 0 && a.mk.Get("stock", h.Code) == nil {
				a.mk.Put(&Quote{Kind: "stock", Code: h.Code, Name: name, Price: h.Price, Prev: h.Price, Source: "nh", At: time.Now().UnixMilli()})
			}
		}
		a.pf.ReplaceNH(ac.No, lots)
		total += len(lots)
	}
	log.Printf("nh import: %d accounts, %d holdings, %d errors", len(accts), total, len(errs))
	if len(errs) > 0 && len(errs) == len(accts) {
		return total, fmt.Errorf("%s", errs[0])
	}
	return total, nil
}

// ---------------------------------------------------------------- 총자산 기록

func (a *App) historyLoop() {
	time.Sleep(20 * time.Second)
	for {
		if v, ok := a.totalValue(); ok && v > 0 {
			a.hist.Add(time.Now(), v)
		}
		time.Sleep(time.Minute - time.Duration(time.Now().Second())*time.Second)
	}
}

// totalValue: 모든 보유 종목의 시세가 있을 때만 총자산을 계산해요
func (a *App) totalValue() (float64, bool) {
	pf := a.pf.Snapshot()
	if len(pf.Lots) == 0 {
		return 0, false
	}
	total := 0.0
	for _, l := range pf.Lots {
		q := a.mk.Get(l.Kind, l.Code)
		if q == nil {
			return 0, false
		}
		total += l.Qty * q.Price
	}
	return total, true
}

// ---------------------------------------------------------------- 화면이 1초마다 묻는 곳

func (a *App) handlePoll(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("q")
	keys := map[string]bool{}
	for _, k := range a.pf.HeldKeys() {
		keys[k] = true
	}
	for _, k := range strings.Split(r.URL.Query().Get("extra"), ",") {
		if k != "" && len(keys) < 400 {
			keys[k] = true
		}
	}
	quotes, qrev := a.mk.Snapshot(keys)
	out := map[string]any{
		"build":  build,
		"shell":  env("SHELL"),
		"rev":    a.pf.Rev(),
		"qrev":   qrev,
		"now":    time.Now().UnixMilli(),
		"open":   stockMarketOpen(time.Now()),
		"nh":     a.nh.Status(),
		"market": a.mk.Status(),
		"update": a.upd.State(),
		"master": a.master.Ready(),
	}
	if since != fmt.Sprint(qrev) {
		out["quotes"] = quotes
	}
	writeJSON(w, out)
}

// sortedKeys is a small helper for stable output.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
