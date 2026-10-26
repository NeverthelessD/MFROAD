package main

// 총자산 기록 — 1분마다 한 점, 하루 한 파일 (Support/history/2026-10-26.json), 7일치만 남겨요.
// 화면의 '오늘 그래프'가 이걸 그려요.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type HistPoint struct {
	T int64   `json:"t"` // 유닉스 초
	V float64 `json:"v"`
}

type History struct {
	dir   string
	mu    sync.Mutex
	day   string
	today []HistPoint
}

func NewHistory(support string) *History {
	h := &History{dir: filepath.Join(support, "history")}
	os.MkdirAll(h.dir, 0o755)
	h.day = time.Now().In(kst).Format("2006-01-02")
	if b, err := os.ReadFile(h.path(h.day)); err == nil {
		json.Unmarshal(b, &h.today)
	}
	return h
}

func (h *History) path(day string) string { return filepath.Join(h.dir, day+".json") }

func (h *History) Add(t time.Time, v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	day := t.In(kst).Format("2006-01-02")
	if day != h.day {
		h.day, h.today = day, nil
		h.prune()
	}
	sec := t.Unix()
	if n := len(h.today); n > 0 && sec-h.today[n-1].T < 30 {
		h.today[n-1].V = v
	} else {
		h.today = append(h.today, HistPoint{T: sec, V: v})
	}
	b, _ := json.Marshal(h.today)
	writeFileAtomic(h.path(h.day), b, 0o600)
}

func (h *History) prune() {
	ents, _ := os.ReadDir(h.dir)
	var days []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".json") {
			days = append(days, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(days)
	for len(days) > 7 {
		os.Remove(h.path(days[0]))
		days = days[1:]
	}
}

// Today: 오늘 기록 (점이 많으면 고르게 줄여요)
func (h *History) Today(max int) []HistPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Now().In(kst).Format("2006-01-02") != h.day {
		return []HistPoint{}
	}
	pts := h.today
	if len(pts) <= max {
		return append([]HistPoint{}, pts...)
	}
	out := make([]HistPoint, 0, max)
	step := float64(len(pts)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		out = append(out, pts[int(float64(i)*step+0.5)])
	}
	return out
}
