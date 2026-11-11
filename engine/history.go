package main

// 총자산 기록
//   - 1분마다 한 점, 하루 한 파일 (Support/history/2026-10-26.json), 8일치만 남겨요 → '오늘' · '주간' 그래프
//   - 하루에 한 점(그날 마지막 값)은 Support/history/daily.json 에 계속 남겨요 → '월간' · '연간' 그래프
// 점마다 전체(v) · 국내주식(s) · 코인(c) 금액을 적어요. 1.0.4 까지의 점에는 v 만 있어요.

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
	T int64    `json:"t"` // 유닉스 초
	V float64  `json:"v"`
	S *float64 `json:"s,omitempty"` // 국내주식
	C *float64 `json:"c,omitempty"` // 코인
}

// DayPoint: 하루 한 점 (그날 마지막으로 적은 값)
type DayPoint struct {
	D string `json:"d"` // 2026-11-11
	HistPoint
}

const histKeepDays = 8 // 분 단위 파일 (오늘 포함) — '주간'(지난 7일)을 다 덮도록

type History struct {
	dir   string
	mu    sync.Mutex
	day   string
	today []HistPoint
	daily []DayPoint
}

func isDayName(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func NewHistory(support string) *History {
	h := &History{dir: filepath.Join(support, "history")}
	os.MkdirAll(h.dir, 0o755)
	h.day = time.Now().In(kst).Format("2006-01-02")
	if b, err := os.ReadFile(h.path(h.day)); err == nil {
		json.Unmarshal(b, &h.today)
	}
	if b, err := os.ReadFile(h.dailyPath()); err == nil {
		json.Unmarshal(b, &h.daily)
	}
	h.backfillDaily()
	return h
}

func (h *History) path(day string) string { return filepath.Join(h.dir, day+".json") }
func (h *History) dailyPath() string      { return filepath.Join(h.dir, "daily.json") }

func (h *History) dayFiles() []string {
	ents, _ := os.ReadDir(h.dir)
	var days []string
	for _, e := range ents {
		n := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasSuffix(e.Name(), ".json") && isDayName(n) {
			days = append(days, n)
		}
	}
	sort.Strings(days)
	return days
}

func (h *History) readDay(day string) []HistPoint {
	var pts []HistPoint
	if b, err := os.ReadFile(h.path(day)); err == nil {
		json.Unmarshal(b, &pts)
	}
	return pts
}

// backfillDaily: daily.json 이 생기기 전(1.0.4 까지)에 쌓인 분 단위 파일에서 하루 한 점을 채워요
func (h *History) backfillDaily() {
	have := map[string]bool{}
	for _, d := range h.daily {
		have[d.D] = true
	}
	changed := false
	for _, day := range h.dayFiles() {
		if have[day] {
			continue
		}
		pts := h.readDay(day)
		if day == h.day {
			pts = h.today
		}
		if len(pts) == 0 {
			continue
		}
		h.daily = append(h.daily, DayPoint{D: day, HistPoint: pts[len(pts)-1]})
		changed = true
	}
	if changed {
		sort.Slice(h.daily, func(i, j int) bool { return h.daily[i].D < h.daily[j].D })
		h.saveDaily()
	}
}

func (h *History) saveDaily() {
	b, _ := json.Marshal(h.daily)
	writeFileAtomic(h.dailyPath(), b, 0o600)
}

// Add: 전체 v = 국내주식 s + 코인 c
func (h *History) Add(t time.Time, s, c float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	day := t.In(kst).Format("2006-01-02")
	if day != h.day {
		h.day, h.today = day, nil
		h.prune()
	}
	p := HistPoint{T: t.Unix(), V: s + c, S: &s, C: &c}
	if n := len(h.today); n > 0 && p.T-h.today[n-1].T < 30 {
		p.T = h.today[n-1].T
		h.today[n-1] = p
	} else {
		h.today = append(h.today, p)
	}
	b, _ := json.Marshal(h.today)
	writeFileAtomic(h.path(h.day), b, 0o600)

	dp := DayPoint{D: day, HistPoint: HistPoint{T: t.Unix(), V: p.V, S: p.S, C: p.C}}
	if n := len(h.daily); n > 0 && h.daily[n-1].D == day {
		h.daily[n-1] = dp
	} else {
		h.daily = append(h.daily, dp)
	}
	h.saveDaily()
}

func (h *History) prune() {
	days := h.dayFiles()
	for len(days) > histKeepDays {
		os.Remove(h.path(days[0]))
		days = days[1:]
	}
}

// thin: 점이 많으면 시간 칸(max개)마다 마지막 점만 남겨요. 첫 점은 늘 남겨요.
func thin(pts []HistPoint, max int) []HistPoint {
	if len(pts) <= max || max < 2 {
		return append([]HistPoint{}, pts...)
	}
	t0, t1 := pts[0].T, pts[len(pts)-1].T
	w := float64(t1-t0) / float64(max-1)
	if w <= 0 {
		return []HistPoint{pts[0], pts[len(pts)-1]}
	}
	out := []HistPoint{pts[0]}
	last := 0
	for _, p := range pts[1:] {
		b := int(float64(p.T-t0)/w) + 1
		if b == last {
			out[len(out)-1] = p
		} else {
			out = append(out, p)
			last = b
		}
	}
	return out
}

// Today: 오늘 기록 (점이 많으면 줄여요)
func (h *History) Today(max int) []HistPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Now().In(kst).Format("2006-01-02") != h.day {
		return []HistPoint{}
	}
	return thin(h.today, max)
}

// Range: day(오늘) · week(지난 7일, 분 단위 기록) · month(지난 30일) · year(지난 365일, 하루 한 점)
func (h *History) Range(r string, max int) []HistPoint {
	now := time.Now()
	switch r {
	case "week":
		cutoff := now.Add(-7 * 24 * time.Hour).Unix()
		h.mu.Lock()
		today, cur := append([]HistPoint{}, h.today...), h.day
		h.mu.Unlock()
		var pts []HistPoint
		for _, day := range h.dayFiles() {
			if day >= cur {
				continue
			}
			for _, p := range h.readDay(day) {
				if p.T >= cutoff {
					pts = append(pts, p)
				}
			}
		}
		if time.Now().In(kst).Format("2006-01-02") == cur {
			pts = append(pts, today...)
		}
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
		return thin(pts, max)
	case "month", "year":
		days := 30
		if r == "year" {
			days = 365
		}
		from := now.In(kst).AddDate(0, 0, -(days - 1)).Format("2006-01-02")
		h.mu.Lock()
		defer h.mu.Unlock()
		out := []HistPoint{}
		for _, d := range h.daily {
			if d.D >= from {
				out = append(out, d.HistPoint)
			}
		}
		return out
	}
	return h.Today(max)
}
