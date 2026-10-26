package main

// 내 자산 = 계좌 + 매수 기록(lot)
//
// 같은 종목을 여러 계좌(나무증권 · 한국투자 · 업비트 · 빗썸 …)에서 다른 평균 단가로 가지고 있을 수 있어서,
// 기록은 '종목 × 계좌'마다 하나씩 저장하고 화면에서 종목별로 합쳐 보여 줘요.
// 나무증권에서 자동으로 불러온 계좌(Source "nh")의 기록은 불러올 때마다 통째로 바뀌고, 직접 고칠 수 없어요.
// 파일: Support/portfolio.json

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Account struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`             // stock | coin
	Source string `json:"source,omitempty"` // "" (직접 입력) | nh (나무증권에서 자동)
	Ext    string `json:"ext,omitempty"`    // 나무증권 계좌번호
}

type Lot struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"` // stock | coin
	Code    string  `json:"code"` // 005930 · BTC
	Name    string  `json:"name"`
	Market  string  `json:"market,omitempty"` // KOSPI · KOSDAQ · ETF · ETN · KRW
	Account string  `json:"account"`
	Qty     float64 `json:"qty"`
	Avg     float64 `json:"avg"`
	Source  string  `json:"source,omitempty"`
	Updated int64   `json:"updated"`
}

type PortfolioData struct {
	V        int       `json:"v"`
	Accounts []Account `json:"accounts"`
	Lots     []Lot     `json:"lots"`
	Rev      int64     `json:"rev"`
}

type Portfolio struct {
	mu   sync.Mutex
	path string
	d    PortfolioData
}

func NewPortfolio(support string) *Portfolio {
	p := &Portfolio{path: filepath.Join(support, "portfolio.json")}
	p.d = PortfolioData{V: 1, Accounts: []Account{}, Lots: []Lot{}}
	if b, err := os.ReadFile(p.path); err == nil {
		var d PortfolioData
		if json.Unmarshal(b, &d) == nil {
			if d.Accounts == nil {
				d.Accounts = []Account{}
			}
			if d.Lots == nil {
				d.Lots = []Lot{}
			}
			d.V = 1
			p.d = d
		} else {
			// 망가진 파일은 지우지 않고 옆에 보관해요
			os.Rename(p.path, p.path+".broken-"+time.Now().Format("20060102-150405"))
		}
	}
	p.d.Rev = time.Now().UnixMilli()
	return p
}

func (p *Portfolio) saveLocked() error {
	p.d.Rev++
	b, _ := json.MarshalIndent(p.d, "", "  ")
	if err := writeFileAtomic(p.path, b, 0o600); err != nil {
		return errors.New("자산 기록을 저장하지 못했어요")
	}
	return nil
}

func (p *Portfolio) Snapshot() PortfolioData {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.d
	d.Accounts = append([]Account{}, p.d.Accounts...)
	d.Lots = append([]Lot{}, p.d.Lots...)
	return d
}

func (p *Portfolio) Rev() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.d.Rev
}

// Held: 시세를 받아야 하는 종목 코드 (정렬)
func (p *Portfolio) Held(kind string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := map[string]bool{}
	for _, l := range p.d.Lots {
		if l.Kind == kind {
			set[l.Code] = true
		}
	}
	return sortedKeys(set)
}

func (p *Portfolio) HeldKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := map[string]bool{}
	for _, l := range p.d.Lots {
		set[qkey(l.Kind, l.Code)] = true
	}
	return sortedKeys(set)
}

func (p *Portfolio) accountLocked(id string) *Account {
	for i := range p.d.Accounts {
		if p.d.Accounts[i].ID == id {
			return &p.d.Accounts[i]
		}
	}
	return nil
}

func validKind(k string) bool { return k == "stock" || k == "coin" }

func cleanName(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	for utf8.RuneCountInString(s) > max {
		r := []rune(s)
		s = string(r[:max])
	}
	return s
}

// SaveAccount: 새 계좌를 만들거나 이름을 바꿔요
func (p *Portfolio) SaveAccount(in Account) (Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in.Name = cleanName(in.Name, 24)
	if in.Name == "" {
		return in, errors.New("계좌 이름을 넣어 주세요")
	}
	if in.ID != "" {
		a := p.accountLocked(in.ID)
		if a == nil {
			return in, errors.New("계좌를 찾지 못했어요")
		}
		for _, o := range p.d.Accounts {
			if o.ID != a.ID && o.Kind == a.Kind && o.Name == in.Name {
				return in, errors.New("같은 이름의 계좌가 이미 있어요")
			}
		}
		a.Name = in.Name
		return *a, p.saveLocked()
	}
	if !validKind(in.Kind) {
		return in, errors.New("계좌 종류가 올바르지 않아요")
	}
	for _, o := range p.d.Accounts {
		if o.Kind == in.Kind && o.Name == in.Name {
			return o, nil // 같은 이름이면 그 계좌를 그대로 써요
		}
	}
	a := Account{ID: "a" + randID(5), Name: in.Name, Kind: in.Kind}
	p.d.Accounts = append(p.d.Accounts, a)
	return a, p.saveLocked()
}

// DeleteAccount: 계좌와 그 계좌의 기록을 함께 지워요
func (p *Portfolio) DeleteAccount(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.accountLocked(id) == nil {
		return errors.New("계좌를 찾지 못했어요")
	}
	accts := p.d.Accounts[:0]
	for _, a := range p.d.Accounts {
		if a.ID != id {
			accts = append(accts, a)
		}
	}
	p.d.Accounts = accts
	lots := p.d.Lots[:0]
	for _, l := range p.d.Lots {
		if l.Account != id {
			lots = append(lots, l)
		}
	}
	p.d.Lots = lots
	return p.saveLocked()
}

// SaveLot: 매수 기록을 새로 넣거나 고쳐요.
// 같은 계좌에 같은 종목 기록이 이미 있으면(새로 넣을 때) 수량을 더하고 평균 단가를 다시 계산해요.
func (p *Portfolio) SaveLot(in Lot) (Lot, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = cleanName(in.Name, 40)
	in.Market = cleanName(in.Market, 10)
	if !validKind(in.Kind) || in.Code == "" || len(in.Code) > 20 {
		return in, false, errors.New("종목을 골라 주세요")
	}
	if math.IsNaN(in.Qty) || math.IsInf(in.Qty, 0) || in.Qty <= 0 {
		return in, false, errors.New("보유 수량을 0보다 크게 넣어 주세요")
	}
	if math.IsNaN(in.Avg) || math.IsInf(in.Avg, 0) || in.Avg < 0 {
		return in, false, errors.New("평균 매수가를 확인해 주세요")
	}
	if in.Qty > 1e12 || in.Avg > 1e13 {
		return in, false, errors.New("숫자가 너무 커요")
	}
	acct := p.accountLocked(in.Account)
	if acct == nil {
		return in, false, errors.New("계좌를 골라 주세요")
	}
	if acct.Kind != in.Kind {
		return in, false, errors.New("이 계좌에는 이 종류의 자산을 넣을 수 없어요")
	}
	if acct.Source == "nh" {
		return in, false, errors.New("나무증권에서 자동으로 불러오는 계좌라 직접 넣을 수 없어요. 다른 계좌를 골라 주세요")
	}
	in.Updated = time.Now().UnixMilli()
	in.Source = ""
	if in.ID != "" {
		for i := range p.d.Lots {
			if p.d.Lots[i].ID == in.ID {
				if p.d.Lots[i].Source == "nh" {
					return in, false, errors.New("나무증권에서 자동으로 불러온 기록은 고칠 수 없어요")
				}
				p.d.Lots[i] = in
				return in, false, p.saveLocked()
			}
		}
		return in, false, errors.New("기록을 찾지 못했어요. 다시 열어 주세요")
	}
	for i := range p.d.Lots {
		l := &p.d.Lots[i]
		if l.Kind == in.Kind && l.Code == in.Code && l.Account == in.Account {
			q := l.Qty + in.Qty
			l.Avg = (l.Qty*l.Avg + in.Qty*in.Avg) / q
			l.Qty = q
			l.Updated = in.Updated
			if in.Name != "" {
				l.Name = in.Name
			}
			return *l, true, p.saveLocked()
		}
	}
	in.ID = "l" + randID(6)
	p.d.Lots = append(p.d.Lots, in)
	return in, false, p.saveLocked()
}

func (p *Portfolio) DeleteLot(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, l := range p.d.Lots {
		if l.ID == id {
			if l.Source == "nh" {
				return errors.New("나무증권에서 자동으로 불러온 기록은 지울 수 없어요")
			}
			p.d.Lots = append(p.d.Lots[:i], p.d.Lots[i+1:]...)
			return p.saveLocked()
		}
	}
	return errors.New("기록을 찾지 못했어요")
}

// ReplaceNH: 나무증권 계좌 하나의 기록을 새로 불러온 것으로 바꿔요 (계좌가 없으면 만들어요)
func (p *Portfolio) ReplaceNH(acctNo string, lots []Lot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := "nh-" + acctNo
	if p.accountLocked(id) == nil {
		name := "나무증권"
		n := 0
		for _, a := range p.d.Accounts {
			if a.Source == "nh" {
				n++
			}
		}
		if n > 0 {
			name = "나무증권 ··" + tail4(acctNo)
		}
		p.d.Accounts = append(p.d.Accounts, Account{ID: id, Name: name, Kind: "stock", Source: "nh", Ext: acctNo})
	}
	// 바뀐 게 없으면 저장하지 않아요
	var old []Lot
	kept := p.d.Lots[:0]
	for _, l := range p.d.Lots {
		if l.Account == id {
			old = append(old, l)
		} else {
			kept = append(kept, l)
		}
	}
	same := len(old) == len(lots)
	if same {
		m := map[string]Lot{}
		for _, l := range old {
			m[l.Code] = l
		}
		for _, l := range lots {
			o, ok := m[l.Code]
			if !ok || math.Abs(o.Qty-l.Qty) > 1e-9 || math.Abs(o.Avg-l.Avg) > 1e-6 {
				same = false
				break
			}
		}
	}
	if same {
		p.d.Lots = append(kept, old...)
		return
	}
	now := time.Now().UnixMilli()
	for _, l := range lots {
		l.ID = "n" + acctNo + "-" + l.Code
		l.Account = id
		l.Kind = "stock"
		l.Source = "nh"
		l.Updated = now
		kept = append(kept, l)
	}
	p.d.Lots = kept
	p.saveLocked()
}

// ---------------------------------------------------------------- API

func (a *App) handleLotSave(w http.ResponseWriter, r *http.Request) {
	var in Lot
	if err := readJSON(r, &in); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if in.Kind == "stock" {
		if s := a.master.Lookup(strings.ToUpper(strings.TrimSpace(in.Code))); s != nil {
			if in.Name == "" {
				in.Name = s.Name
			}
			in.Market = s.Market
		}
	} else {
		if c := a.coins.Lookup(in.Code); c != nil && in.Name == "" {
			in.Name = c.Name
		}
		in.Market = "KRW"
	}
	lot, merged, err := a.pf.SaveLot(in)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.mk.Wake()
	writeJSON(w, map[string]any{"ok": true, "lot": lot, "merged": merged, "rev": a.pf.Rev()})
}

func (a *App) handleLotDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	readJSON(r, &in)
	if err := a.pf.DeleteLot(in.ID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "rev": a.pf.Rev()})
}

func (a *App) handleAccountSave(w http.ResponseWriter, r *http.Request) {
	var in Account
	if err := readJSON(r, &in); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	acct, err := a.pf.SaveAccount(in)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "account": acct, "rev": a.pf.Rev()})
}

func (a *App) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	readJSON(r, &in)
	if err := a.pf.DeleteAccount(in.ID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "rev": a.pf.Rev()})
}
