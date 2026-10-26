package main

// 국내주식 종목 목록 (검색용) — NH투자증권이 매일 배포하는 종목마스터 m_new_stock.mst
//   https://www.nhplug.com/instruments/m_new_stock.mst (인증 필요 없음)
//   한 종목 237바이트 고정 길이, CP949 글자, 끝 1바이트 LF. 필드 위치는 nhplug-sdk 의 m_new_stock.h 를 따랐어요.
//   12시간마다 새로 받고, 못 받으면 저장해 둔 것을 써요 (Support/cache/m_new_stock.mst).
//   전일 종가(sPrePrice)도 들어 있어서, 나무증권을 연결하기 전에는 이 값을 현재가 대신 보여 줘요.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/encoding/korean"
)

const masterRecord = 237

type StockInfo struct {
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Eng    string  `json:"eng"`
	Market string  `json:"market"` // KOSPI | KOSDAQ | ETF | ETN
	Prev   float64 `json:"prev"`
	K200   bool    `json:"-"`
	key    string  // 검색용 (소문자 · 빈칸 없음)
	cho    string  // 초성
	ekey   string
}

type Master struct {
	support  string
	mu       sync.Mutex
	list     []*StockInfo
	byCode   map[string]*StockInfo
	loadedAt time.Time
	err      string
	loading  bool
}

func NewMaster(support string) *Master {
	return &Master{support: support, byCode: map[string]*StockInfo{}}
}

func masterURL() string {
	if u := env("MASTER_URL"); u != "" && isLocalURL(u) {
		return u
	}
	return "https://www.nhplug.com/instruments/m_new_stock.mst"
}

func (m *Master) cachePath() string { return filepath.Join(m.support, "cache", "m_new_stock.mst") }

func (m *Master) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.list) > 0
}

func (m *Master) LoadedAt() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadedAt.UnixMilli()
}

func (m *Master) DiagLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.list) == 0 {
		return "없음 " + m.err
	}
	return fmt.Sprintf("%d종목 · %s · %s", len(m.list), m.loadedAt.Format("01-02 15:04"), m.err)
}

// Ensure: 저장해 둔 목록을 먼저 읽고, 12시간이 지났으면 새로 받아요. 그 뒤로 12시간마다 다시 확인해요.
func (m *Master) Ensure() {
	m.loadCache()
	fails := 0
	for {
		st, err := os.Stat(m.cachePath())
		if err != nil || time.Since(st.ModTime()) > 12*time.Hour || !m.Ready() {
			if err := m.download(); err != nil {
				m.mu.Lock()
				m.err = err.Error()
				m.mu.Unlock()
				log.Printf("master: %v", err)
				fails++
				time.Sleep(retryWait(fails, m.Ready()))
				continue
			}
		}
		fails = 0
		time.Sleep(time.Hour)
	}
}

// retryWait: 목록이 아직 하나도 없으면(처음 켠 때) 금방 다시 받아 보고, 있으면 천천히 다시 시도해요.
func retryWait(fails int, have bool) time.Duration {
	if have {
		return 2 * time.Minute
	}
	switch {
	case fails <= 1:
		return 5 * time.Second
	case fails <= 3:
		return 15 * time.Second
	case fails <= 6:
		return 45 * time.Second
	}
	return 2 * time.Minute
}

// WaitReady: 처음 켠 직후 검색하면 목록을 받을 때까지 잠깐(최대 d) 기다려요.
func (m *Master) WaitReady(d time.Duration) bool { return waitFor(m.Ready, d) }

func waitFor(ok func() bool, d time.Duration) bool {
	end := time.Now().Add(d)
	for !ok() {
		if time.Now().After(end) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
	return true
}

func (m *Master) loadCache() {
	b, err := os.ReadFile(m.cachePath())
	if err != nil {
		return
	}
	if err := m.parse(b); err != nil {
		log.Printf("master cache: %v", err)
		return
	}
	if st, err := os.Stat(m.cachePath()); err == nil {
		m.mu.Lock()
		m.loadedAt = st.ModTime()
		m.mu.Unlock()
	}
}

func (m *Master) download() error {
	req, _ := http.NewRequest("GET", masterURL(), nil)
	req.Header.Set("User-Agent", "MFROAD/"+build)
	resp, err := (&http.Client{Timeout: 40 * time.Second}).Do(req)
	if err != nil {
		return errors.New("국내주식 종목 목록을 받지 못했어요 (인터넷 연결 확인)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("국내주식 종목 목록을 받지 못했어요 (HTTP %d)", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return errors.New("국내주식 종목 목록을 받다가 끊겼어요")
	}
	if err := m.parse(b); err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(m.cachePath()), 0o755)
	writeFileAtomic(m.cachePath(), b, 0o644)
	m.mu.Lock()
	m.loadedAt = time.Now()
	m.err = ""
	m.mu.Unlock()
	log.Printf("master: %d stocks", len(m.list))
	return nil
}

func field(rec []byte, off, n int) []byte { return bytes.TrimRight(rec[off:off+n], " \x00") }

func (m *Master) parse(b []byte) error {
	if len(b) < masterRecord || len(b)%masterRecord != 0 {
		return errors.New("국내주식 종목 목록 파일 모양이 달라요")
	}
	dec := korean.EUCKR.NewDecoder()
	var list []*StockInfo
	by := map[string]*StockInfo{}
	for off := 0; off+masterRecord <= len(b); off += masterRecord {
		rec := b[off : off+masterRecord]
		code := string(field(rec, 0, 6))
		if code == "" {
			continue
		}
		nameB, _ := dec.Bytes(field(rec, 7, 41))
		name := strings.TrimSpace(string(nameB))
		k200 := false
		if strings.HasPrefix(name, "*") || strings.HasPrefix(name, "#") {
			k200 = strings.HasPrefix(name, "*")
			name = strings.TrimSpace(name[1:])
		}
		eng := strings.TrimLeft(strings.TrimSpace(string(field(rec, 48, 41))), "*#")
		prev, _ := strconv.ParseFloat(strings.TrimLeft(string(field(rec, 152, 7)), "0"), 64)
		mk := "KOSPI"
		switch {
		case rec[6] == 'A' || rec[165] == 'E':
			mk = "ETN"
		case rec[165] == '8':
			mk = "ETF"
		case rec[6] == '4':
			mk = "KOSDAQ"
		}
		s := &StockInfo{Code: code, Name: name, Eng: eng, Market: mk, Prev: prev, K200: k200}
		s.key = searchKey(name)
		s.cho = chosung(name)
		s.ekey = searchKey(eng)
		list = append(list, s)
		by[code] = s
	}
	if len(list) < 100 {
		return errors.New("국내주식 종목 목록이 비어 있어요")
	}
	m.mu.Lock()
	m.list, m.byCode = list, by
	m.mu.Unlock()
	return nil
}

func (m *Master) Lookup(code string) *StockInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byCode[code]
}

// Search: 이름 · 초성(ㅅㅅㅈㅈ) · 코드 · 영문 이름으로 찾아요
func (m *Master) Search(q string, limit int) ([]*StockInfo, error) {
	m.mu.Lock()
	list := m.list
	errMsg := m.err
	m.mu.Unlock()
	if len(list) == 0 {
		if errMsg == "" {
			errMsg = "국내주식 종목 목록을 준비하고 있어요. 잠시 뒤 다시 찾아 주세요"
		}
		return nil, errors.New(errMsg)
	}
	key := searchKey(q)
	if key == "" {
		return nil, nil
	}
	choOnly := isChosungQuery(key)
	type hit struct {
		s     *StockInfo
		score int
	}
	var hits []hit
	for _, s := range list {
		sc := 0
		switch {
		case s.Code == strings.ToUpper(key):
			sc = 1000
		case s.key == key:
			sc = 900
		case strings.HasPrefix(s.key, key):
			sc = 700
		case strings.HasPrefix(s.Code, strings.ToUpper(key)):
			sc = 650
		case strings.Contains(s.key, key):
			sc = 500
		case choOnly && strings.HasPrefix(s.cho, key):
			sc = 450
		case choOnly && strings.Contains(s.cho, key):
			sc = 300
		case len(key) >= 2 && strings.Contains(s.ekey, key):
			sc = 250
		}
		if sc == 0 {
			continue
		}
		if s.Market == "KOSPI" || s.Market == "KOSDAQ" {
			sc += 40 // 같은 점수면 일반 주식을 ETF 보다 앞에
		}
		if s.K200 {
			sc += 20
		}
		sc -= len([]rune(s.Name)) // 짧은 이름(정확히 맞는 쪽) 먼저
		hits = append(hits, hit{s, sc})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*StockInfo, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out, nil
}

func searchKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == '(' || r == ')' || r == '&' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var choTable = []rune("ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ")

func chosung(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 0xAC00 && r <= 0xD7A3 {
			b.WriteRune(choTable[(r-0xAC00)/588])
		} else if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isChosungQuery(s string) bool {
	has := false
	for _, r := range s {
		if r >= 0x3131 && r <= 0x314E {
			has = true
		} else if r >= 0xAC00 && r <= 0xD7A3 {
			return false
		}
	}
	return has
}
