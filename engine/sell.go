package main

// 팔기 · 판 돈(현금)
//
// 주식이나 코인을 팔면: 판 금액(수량 × 판 가격)에서 증권사 · 거래소 수수료와 (주식이면) 증권거래세를 빼고,
// 받은 금액을 그 계좌의 현금(Account.Cash)에 넣어요. 현금도 총 자산에 들어가므로
// 팔아도 총 자산은 '받은 금액'만큼 그대로 남아요. 판 기록(Sale)은 되돌릴 수 있어요.

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

// 증권거래세(농어촌특별세 포함, 2026년 1월 1일 이후 매도분): 코스피 0.05%+0.15% · 코스닥 0.20% → 둘 다 0.20%.
// ETF · ETN 은 증권거래세가 없어요.
const stockSellTaxRate = 0.20

func sellTaxRate(kind, market string) float64 {
	if kind != "stock" {
		return 0
	}
	switch strings.ToUpper(market) {
	case "ETF", "ETN":
		return 0
	}
	return stockSellTaxRate
}

// sellAmounts: 주식은 수수료 · 세금을 원 단위 아래 버림(증권사 계산 방식), 코인도 원 단위로 버림
func sellAmounts(kind, market string, qty, price, feeRate float64) (gross, fee, taxRate, tax, net float64) {
	gross = qty * price
	if kind == "stock" {
		gross = math.Round(gross)
	}
	fee = math.Floor(gross * feeRate / 100)
	taxRate = sellTaxRate(kind, market)
	tax = math.Floor(gross * taxRate / 100)
	net = gross - fee - tax
	return
}

type SellInput struct {
	LotID   string   `json:"lot"`
	Qty     float64  `json:"qty"`
	Price   float64  `json:"price"`
	FeeRate *float64 `json:"feeRate"` // % — 넣으면 그 계좌의 수수료율로 기억해요
}

func badNum(x float64) bool { return math.IsNaN(x) || math.IsInf(x, 0) }

// Sell: 기록에서 수량을 빼고 받은 금액을 계좌 현금에 넣어요
func (p *Portfolio) Sell(in SellInput) (Sale, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := -1
	for i := range p.d.Lots {
		if p.d.Lots[i].ID == in.LotID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Sale{}, errors.New("기록을 찾지 못했어요. 다시 열어 주세요")
	}
	l := p.d.Lots[idx]
	acct := p.accountLocked(l.Account)
	if acct == nil {
		return Sale{}, errors.New("계좌를 찾지 못했어요")
	}
	if badNum(in.Qty) || in.Qty <= 0 {
		return Sale{}, errors.New("판 수량을 0보다 크게 넣어 주세요")
	}
	if in.Qty > l.Qty*(1+1e-9) {
		return Sale{}, errors.New("가지고 있는 수량보다 많이 팔 수 없어요")
	}
	if in.Qty > l.Qty {
		in.Qty = l.Qty
	}
	if badNum(in.Price) || in.Price <= 0 || in.Price > 1e13 {
		return Sale{}, errors.New("판 가격을 넣어 주세요")
	}
	rate := 0.0
	if acct.Fee != nil {
		rate = *acct.Fee
	}
	if in.FeeRate != nil {
		if badNum(*in.FeeRate) || *in.FeeRate < 0 || *in.FeeRate > 5 {
			return Sale{}, errors.New("수수료율은 0 ~ 5% 사이로 넣어 주세요")
		}
		rate = *in.FeeRate
		r := rate
		acct.Fee = &r
	}
	gross, fee, taxRate, tax, net := sellAmounts(l.Kind, l.Market, in.Qty, in.Price, rate)
	s := Sale{
		ID: "s" + randID(6), At: time.Now().UnixMilli(), Kind: l.Kind, Code: l.Code, Name: l.Name, Market: l.Market,
		Account: l.Account, Qty: in.Qty, Price: in.Price, Avg: l.Avg, Gross: gross, FeeRate: rate, Fee: fee,
		TaxRate: taxRate, Tax: tax, Net: net, Realized: net - in.Qty*l.Avg, LotID: l.ID, Source: l.Source,
	}
	left := l.Qty - in.Qty
	if left <= l.Qty*1e-9 || left < 1e-12 {
		p.d.Lots = append(p.d.Lots[:idx], p.d.Lots[idx+1:]...)
	} else {
		p.d.Lots[idx].Qty = left
		p.d.Lots[idx].Updated = s.At
	}
	acct.Cash = roundCash(acct.Cash + net)
	p.d.Sales = append(p.d.Sales, s)
	if len(p.d.Sales) > 2000 {
		p.d.Sales = p.d.Sales[len(p.d.Sales)-2000:]
	}
	return s, p.saveLocked()
}

func roundCash(x float64) float64 {
	x = math.Round(x*100) / 100
	if math.Abs(x) < 0.005 {
		return 0
	}
	return x
}

// UndoSale: 판 기록을 지우고 수량 · 현금을 되돌려요 (잘못 넣었을 때)
func (p *Portfolio) UndoSale(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	si := -1
	for i := range p.d.Sales {
		if p.d.Sales[i].ID == id {
			si = i
			break
		}
	}
	if si < 0 {
		return errors.New("판 기록을 찾지 못했어요")
	}
	s := p.d.Sales[si]
	acct := p.accountLocked(s.Account)
	if acct == nil {
		return errors.New("계좌를 찾지 못했어요")
	}
	acct.Cash = roundCash(math.Max(0, acct.Cash-s.Net))
	// 수량을 그 기록에 되돌려요 (나무증권 계좌는 다음에 불러올 때 계좌 그대로 바뀌어요)
	back := false
	for i := range p.d.Lots {
		l := &p.d.Lots[i]
		if l.ID == s.LotID || (l.Account == s.Account && l.Kind == s.Kind && l.Code == s.Code) {
			q := l.Qty + s.Qty
			l.Avg = (l.Qty*l.Avg + s.Qty*s.Avg) / q
			l.Qty = q
			l.Updated = time.Now().UnixMilli()
			back = true
			break
		}
	}
	if !back {
		id := s.LotID
		if id == "" {
			id = "l" + randID(6)
		}
		p.d.Lots = append(p.d.Lots, Lot{ID: id, Kind: s.Kind, Code: s.Code, Name: s.Name, Market: s.Market,
			Account: s.Account, Qty: s.Qty, Avg: s.Avg, Source: s.Source, Updated: time.Now().UnixMilli()})
	}
	p.d.Sales = append(p.d.Sales[:si], p.d.Sales[si+1:]...)
	return p.saveLocked()
}

// SetCash: 계좌 현금을 직접 맞춰요 (출금했거나 다른 데 썼을 때)
func (p *Portfolio) SetCash(acctID string, cash float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.accountLocked(acctID)
	if a == nil {
		return errors.New("계좌를 찾지 못했어요")
	}
	if badNum(cash) || cash < 0 || cash > 1e15 {
		return errors.New("금액을 확인해 주세요")
	}
	a.Cash = roundCash(cash)
	return p.saveLocked()
}

// spendCashLocked: 새로 산 금액만큼 그 계좌 현금에서 빼요 (현금보다 많으면 0 까지만)
func (p *Portfolio) spendCashLocked(acctID string, amount float64) {
	if a := p.accountLocked(acctID); a != nil && amount > 0 {
		a.Cash = roundCash(math.Max(0, a.Cash-amount))
	}
}

// CashBy: 국내주식 · 코인 계좌 현금 합
func (d PortfolioData) CashBy() (stock, coin float64) {
	for _, a := range d.Accounts {
		if a.Kind == "coin" {
			coin += a.Cash
		} else {
			stock += a.Cash
		}
	}
	return
}

// ---------------------------------------------------------------- API

func (a *App) handleLotSell(w http.ResponseWriter, r *http.Request) {
	var in SellInput
	if err := readJSON(r, &in); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	s, err := a.pf.Sell(in)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.mk.Wake()
	writeJSON(w, map[string]any{"ok": true, "sale": s, "rev": a.pf.Rev()})
}

func (a *App) handleSaleUndo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
	}
	readJSON(r, &in)
	if err := a.pf.UndoSale(in.ID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.mk.Wake()
	writeJSON(w, map[string]any{"ok": true, "rev": a.pf.Rev()})
}

func (a *App) handleCashSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Account string  `json:"account"`
		Cash    float64 `json:"cash"`
	}
	if err := readJSON(r, &in); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if err := a.pf.SetCash(in.Account, in.Cash); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "rev": a.pf.Rev()})
}
