package main

// 업비트 공개 시세 (키 필요 없음) — 원화(KRW) 마켓만
//   마켓 목록  GET https://api.upbit.com/v1/market/all?isDetails=false  (12시간 저장: Support/cache/upbit-markets.json)
//   현재가    GET https://api.upbit.com/v1/ticker?markets=KRW-BTC,KRW-ETH  (한 번에 여러 개)

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type CoinInfo struct {
	Code string `json:"code"` // BTC
	Name string `json:"name"` // 비트코인
	Eng  string `json:"eng"`
	key  string
	cho  string
	ekey string
}

type Upbit struct {
	support  string
	mu       sync.Mutex
	list     []*CoinInfo
	byCode   map[string]*CoinInfo
	loadedAt time.Time
	err      string
	client   *http.Client
}

func NewUpbit(support string) *Upbit {
	return &Upbit{support: support, byCode: map[string]*CoinInfo{}, client: &http.Client{Timeout: 10 * time.Second}}
}

func upbitBase() string {
	if u := env("UPBIT_BASE"); u != "" && isLocalURL(u) {
		return strings.TrimRight(u, "/")
	}
	return "https://api.upbit.com"
}

func (u *Upbit) get(path string, v any) error {
	req, _ := http.NewRequest("GET", upbitBase()+path, nil)
	req.Header.Set("accept", "application/json")
	req.Header.Set("User-Agent", "MFROAD/"+build)
	resp, err := u.client.Do(req)
	if err != nil {
		return errors.New("업비트에 연결하지 못했어요. 인터넷 연결을 확인해 주세요")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == 429 {
		return errors.New("업비트에 너무 자주 물어봤어요. 잠시 뒤 다시 받을게요")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("업비트 응답 오류 (HTTP %d)", resp.StatusCode)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return errors.New("업비트 응답을 읽지 못했어요")
	}
	return nil
}

func (u *Upbit) cachePath() string { return filepath.Join(u.support, "cache", "upbit-markets.json") }

type upbitMarket struct {
	Market  string `json:"market"`
	Korean  string `json:"korean_name"`
	English string `json:"english_name"`
}

func (u *Upbit) setList(ms []upbitMarket) {
	var list []*CoinInfo
	by := map[string]*CoinInfo{}
	for _, m := range ms {
		if !strings.HasPrefix(m.Market, "KRW-") {
			continue
		}
		c := &CoinInfo{Code: strings.TrimPrefix(m.Market, "KRW-"), Name: m.Korean, Eng: m.English}
		c.key, c.cho, c.ekey = searchKey(c.Name), chosung(c.Name), searchKey(c.Eng)
		list = append(list, c)
		by[c.Code] = c
	}
	u.mu.Lock()
	u.list, u.byCode = list, by
	u.mu.Unlock()
}

// EnsureMarkets: 저장해 둔 목록을 읽고, 12시간이 지났으면 새로 받아요.
func (u *Upbit) EnsureMarkets() {
	if b, err := os.ReadFile(u.cachePath()); err == nil {
		var ms []upbitMarket
		if json.Unmarshal(b, &ms) == nil {
			u.setList(ms)
			if st, err := os.Stat(u.cachePath()); err == nil {
				u.mu.Lock()
				u.loadedAt = st.ModTime()
				u.mu.Unlock()
			}
		}
	}
	fails := 0
	for {
		u.mu.Lock()
		stale := len(u.list) == 0 || time.Since(u.loadedAt) > 12*time.Hour
		u.mu.Unlock()
		if stale {
			var ms []upbitMarket
			if err := u.get("/v1/market/all?isDetails=false", &ms); err != nil {
				u.mu.Lock()
				u.err = err.Error()
				u.mu.Unlock()
				fails++
				time.Sleep(retryWait(fails, u.Ready()))
				continue
			}
			fails = 0
			u.setList(ms)
			b, _ := json.Marshal(ms)
			os.MkdirAll(filepath.Dir(u.cachePath()), 0o755)
			writeFileAtomic(u.cachePath(), b, 0o644)
			u.mu.Lock()
			u.loadedAt, u.err = time.Now(), ""
			u.mu.Unlock()
		}
		time.Sleep(time.Hour)
	}
}

func (u *Upbit) Lookup(code string) *CoinInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.byCode[strings.ToUpper(code)]
}

func (u *Upbit) Search(q string, limit int) ([]*CoinInfo, error) {
	u.mu.Lock()
	list := u.list
	errMsg := u.err
	u.mu.Unlock()
	if len(list) == 0 {
		if errMsg == "" {
			errMsg = "코인 목록을 준비하고 있어요. 잠시 뒤 다시 찾아 주세요"
		}
		return nil, errors.New(errMsg)
	}
	key := searchKey(q)
	if key == "" {
		return nil, nil
	}
	up := strings.ToUpper(key)
	choOnly := isChosungQuery(key)
	type hit struct {
		c     *CoinInfo
		score int
	}
	var hits []hit
	for _, c := range list {
		sc := 0
		switch {
		case c.Code == up:
			sc = 1000
		case c.key == key:
			sc = 900
		case strings.HasPrefix(c.key, key):
			sc = 700
		case strings.HasPrefix(c.Code, up):
			sc = 650
		case strings.Contains(c.key, key):
			sc = 500
		case choOnly && strings.HasPrefix(c.cho, key):
			sc = 450
		case strings.HasPrefix(c.ekey, key):
			sc = 400
		case len(key) >= 3 && strings.Contains(c.ekey, key):
			sc = 250
		}
		if sc == 0 {
			continue
		}
		sc -= len([]rune(c.Name))
		hits = append(hits, hit{c, sc})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*CoinInfo, len(hits))
	for i, h := range hits {
		out[i] = h.c
	}
	return out, nil
}

type upbitTicker struct {
	Market            string  `json:"market"`
	TradePrice        float64 `json:"trade_price"`
	PrevClosing       float64 `json:"prev_closing_price"`
	SignedChangePrice float64 `json:"signed_change_price"`
	SignedChangeRate  float64 `json:"signed_change_rate"`
	Timestamp         int64   `json:"timestamp"`
}

// Tickers: 여러 코인 현재가를 한 번에 (없는 코드는 빼고 물어요)
func (u *Upbit) Tickers(codes []string) ([]*Quote, error) {
	var markets []string
	for _, c := range codes {
		c = strings.ToUpper(c)
		if u.Lookup(c) == nil && u.Ready() {
			continue // 업비트 원화 마켓에 없는 코인
		}
		markets = append(markets, "KRW-"+c)
	}
	if len(markets) == 0 {
		return nil, nil
	}
	var out []*Quote
	for i := 0; i < len(markets); i += 100 {
		end := min(i+100, len(markets))
		var ts []upbitTicker
		if err := u.get("/v1/ticker?markets="+strings.Join(markets[i:end], ","), &ts); err != nil {
			return out, err
		}
		for _, t := range ts {
			code := strings.TrimPrefix(t.Market, "KRW-")
			name := ""
			if c := u.Lookup(code); c != nil {
				name = c.Name
			}
			at := t.Timestamp
			if at == 0 {
				at = time.Now().UnixMilli()
			}
			out = append(out, &Quote{Kind: "coin", Code: code, Name: name, Price: t.TradePrice, Change: t.SignedChangePrice,
				Rate: t.SignedChangeRate * 100, Prev: t.PrevClosing, Source: "upbit", At: at})
		}
	}
	return out, nil
}

// WaitReady: 처음 켠 직후 검색하면 코인 목록을 받을 때까지 잠깐(최대 d) 기다려요.
func (u *Upbit) WaitReady(d time.Duration) bool { return waitFor(u.Ready, d) }

func (u *Upbit) Ready() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.list) > 0
}
