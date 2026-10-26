package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/korean"
)

func testApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	app = &App{support: dir, token: "t"}
	app.loadSettings()
	return dir
}

// ---------------------------------------------------------------- 종목 마스터

func masterRec(code, name string, mkt byte, etf bool, prev int) []byte {
	r := []byte(strings.Repeat(" ", masterRecord))
	copy(r[0:], code)
	r[6] = mkt
	nb, _ := korean.EUCKR.NewEncoder().Bytes([]byte(name))
	copy(r[7:48], nb)
	copy(r[48:], "ENG")
	copy(r[152:159], []byte(strings.Repeat("0", 7-len(itoa(prev)))+itoa(prev)))
	if etf {
		r[165] = '8'
	} else {
		r[165] = '0'
	}
	r[236] = '\n'
	return r
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestMasterParseAndSearch(t *testing.T) {
	var b []byte
	b = append(b, masterRec("005930", "*삼성전자", '1', false, 71000)...)
	b = append(b, masterRec("005935", "삼성전자우", '1', false, 58500)...)
	b = append(b, masterRec("069500", "KODEX 200", '1', true, 35900)...)
	b = append(b, masterRec("247540", "#에코프로비엠", '4', false, 114800)...)
	for i := 0; i < 120; i++ {
		b = append(b, masterRec("9"+strings.Repeat("0", 4)+string(rune('0'+i%10)), "시험종목", '1', false, 1000)...)
	}
	m := NewMaster(t.TempDir())
	if err := m.parse(b); err != nil {
		t.Fatal(err)
	}
	s := m.Lookup("005930")
	if s == nil || s.Name != "삼성전자" || s.Prev != 71000 || !s.K200 || s.Market != "KOSPI" {
		t.Fatalf("삼성전자 읽기 실패: %+v", s)
	}
	if m.Lookup("069500").Market != "ETF" || m.Lookup("247540").Market != "KOSDAQ" || m.Lookup("247540").Name != "에코프로비엠" {
		t.Fatal("시장 구분 실패")
	}
	for q, want := range map[string]string{"삼성": "005930", "ㅅㅅㅈㅈ": "005930", "005935": "005935", "kodex200": "069500", "에코": "247540"} {
		got, err := m.Search(q, 5)
		if err != nil || len(got) == 0 || got[0].Code != want {
			t.Errorf("검색 %q → %v (원한 것 %s)", q, got, want)
		}
	}
	if err := m.parse(b[:100]); err == nil {
		t.Fatal("망가진 파일을 받아들였어요")
	}
}

// ---------------------------------------------------------------- 포트폴리오

func TestPortfolioLotsAndAccounts(t *testing.T) {
	dir := testApp(t)
	p := NewPortfolio(dir)
	a1, _ := p.SaveAccount(Account{Name: "한국투자", Kind: "stock"})
	a2, _ := p.SaveAccount(Account{Name: " 한국투자 ", Kind: "stock"})
	if a1.ID != a2.ID {
		t.Fatal("같은 이름의 계좌는 하나로")
	}
	c1, _ := p.SaveAccount(Account{Name: "업비트", Kind: "coin"})
	if _, _, err := p.SaveLot(Lot{Kind: "stock", Code: "005930", Account: c1.ID, Qty: 1, Avg: 1}); err == nil {
		t.Fatal("코인 계좌에 주식을 넣으면 안 돼요")
	}
	if _, _, err := p.SaveLot(Lot{Kind: "stock", Code: "005930", Account: a1.ID, Qty: 0, Avg: 1}); err == nil {
		t.Fatal("수량 0은 안 돼요")
	}
	l, merged, err := p.SaveLot(Lot{Kind: "stock", Code: "005930", Account: a1.ID, Qty: 30, Avg: 70000})
	if err != nil || merged {
		t.Fatal(err)
	}
	l2, merged, _ := p.SaveLot(Lot{Kind: "stock", Code: "005930", Account: a1.ID, Qty: 10, Avg: 60000})
	if !merged || l2.ID != l.ID || l2.Qty != 40 || l2.Avg != 67500 {
		t.Fatalf("합치기 실패: %+v", l2)
	}
	// 나무증권에서 같은 종목을 불러와도 따로 남아요
	p.ReplaceNH("20012345678", []Lot{{Code: "005930", Name: "삼성전자", Qty: 120, Avg: 61500}, {Code: "035420", Qty: 20, Avg: 168000}})
	snap := p.Snapshot()
	n := 0
	for _, x := range snap.Lots {
		if x.Code == "005930" {
			n++
		}
	}
	if n != 2 || len(snap.Accounts) != 3 {
		t.Fatalf("계좌별로 따로 저장해야 해요: %+v", snap)
	}
	rev := p.Rev()
	p.ReplaceNH("20012345678", []Lot{{Code: "005930", Name: "삼성전자", Qty: 120, Avg: 61500}, {Code: "035420", Qty: 20, Avg: 168000}})
	if p.Rev() != rev {
		t.Fatal("바뀐 게 없으면 저장하지 않아요")
	}
	if err := p.DeleteLot("n20012345678-005930"); err == nil {
		t.Fatal("자동으로 불러온 기록은 지울 수 없어요")
	}
	if _, _, err := p.SaveLot(Lot{Kind: "stock", Code: "000660", Account: "nh-20012345678", Qty: 1, Avg: 1}); err == nil {
		t.Fatal("자동 계좌에는 직접 넣을 수 없어요")
	}
	// 다시 읽어도 같아요
	p2 := NewPortfolio(dir)
	if len(p2.Snapshot().Lots) != 3 {
		t.Fatal("파일에서 다시 읽기 실패")
	}
	if err := p2.DeleteAccount(a1.ID); err != nil || len(p2.Snapshot().Lots) != 2 {
		t.Fatal("계좌를 지우면 기록도 함께")
	}
	if fi, _ := os.Stat(p.path); fi.Mode().Perm() != 0o600 {
		t.Fatal("자산 파일은 나만 읽을 수 있게 (0600)")
	}
}

// ---------------------------------------------------------------- 나무증권

func TestNHTokenCacheAndBalance(t *testing.T) {
	dir := testApp(t)
	var tokens, calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/oauth2/token":
			if r.URL.Query().Get("appkey") == "BAD" {
				w.WriteHeader(403)
				w.Write([]byte(`{"error_code":"IGW40031","error_description":"유효하지 않은 AppKey"}`))
				return
			}
			n := tokens.Add(1)
			w.Write([]byte(`{"access_token":"tok` + itoa(int(n)) + `","expires_in":86400}`))
		case "/n2/acctinfo":
			calls.Add(1)
			if r.Header.Get("authorization") == "Bearer tok1" && tokens.Load() == 1 && calls.Load() == 1 {
				// 처음 한 번은 '유효하지 않은 토큰' → 한 번만 새로 받고 다시 시도해야 해요
				w.WriteHeader(401)
				w.Write([]byte(`{"error_code":"IGW40043","error_description":"유효하지 않은 token"}`))
				return
			}
			w.Write([]byte(`{"Output_0":[{"acct_no":"111","acct_type":"01"},{"acct_no":"333","acct_type":"03"}]}`))
		case "/krstock/inquiry/v1/balance":
			if r.Header.Get("cts") == "" {
				w.Header().Set("cts", "NEXT")
				w.Header().Set("cts_flag", "Y")
				// 한 주 값(매입가격)으로 오는 경우
				w.Write([]byte(`{"Output_1":[{"iem_cd":"A005930","iem_nm":"삼성전자","itg_bnc_qty":"120","phs_pr":61500,"now_pr":72300,"eal_amt":8676000,"eal_pls_amt":1296000}]}`))
				return
			}
			w.Header().Set("cts_flag", "N")
			// 전체 금액으로 오는 경우도 평균 단가로 바꿔요
			w.Write([]byte(`{"Output_1":{"iem_cd":"035420","iem_nm":"NAVER","itg_bnc_qty":20,"phs_pr":3360000,"now_pr":189200,"eal_amt":3784000,"eal_pls_amt":424000}}`))
		case "/krstock/quote/v1/currentPrice":
			w.Write([]byte(`{"Output_0":{"iem_cd":"005930","iem_nm":"삼성전자","stck_prpr":"72,300","prdy_vrss_sign":"5","prdy_vrss":1300,"prdy_ctrt":1.77}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	t.Setenv("MFROAD_NH_BASE", srv.URL)
	t.Setenv("MFROAD_TEST_SECRETS", "file")

	n := NewNH(dir)
	if err := n.SaveKeys("BAD", "x", ""); err == nil || !strings.Contains(err.Error(), "맞지 않아요") {
		t.Fatalf("틀린 키: %v", err)
	}
	if v, _ := secretGet("nh-appkey"); v != "" {
		t.Fatal("틀린 키를 저장했어요")
	}
	if err := n.SaveKeys("KEY1234", "SECRET", ""); err != nil {
		t.Fatal(err)
	}
	accts, err := n.Accounts()
	if err != nil || len(accts) != 1 || accts[0].No != "111" {
		t.Fatalf("계좌: %v %v", accts, err)
	}
	if tokens.Load() != 2 {
		t.Fatalf("401 뒤에 한 번만 새로 받아야 해요: %d", tokens.Load())
	}
	hs, err := n.Balance("111")
	if err != nil || len(hs) != 2 {
		t.Fatalf("잔고: %v %v", hs, err)
	}
	if hs[0].Code != "005930" || hs[0].Qty != 120 || hs[0].Avg != 61500 || hs[1].Avg != 168000 {
		t.Fatalf("평균 단가: %+v", hs)
	}
	q, err := n.Price("005930")
	if err != nil || q.Price != 72300 || q.Rate != -1.77 || q.Change != -1300 {
		t.Fatalf("현재가: %+v %v", q, err)
	}
	// 앱을 다시 켜도(새 NH) 저장된 토큰을 써요
	n2 := NewNH(dir)
	n2.LoadKeys()
	if _, err := n2.Accounts(); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 {
		t.Fatalf("다시 켰을 때 토큰을 새로 받았어요: %d", tokens.Load())
	}
	// 주문 같은 다른 주소는 막혀 있어요
	if _, err := n2.call("/krstock/order/v1/cashBuy", nil, "", ""); err == nil {
		t.Fatal("주문 API 가 막혀 있지 않아요")
	}
	b, _ := os.ReadFile(n.tokenPath())
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("토큰 파일에 비밀값이 들어갔어요")
	}
}

func TestCheckHost(t *testing.T) {
	if checkHost("https://api.nhplug.com:8443/n2/acctinfo") != nil || checkHost("https://api.n2plug.com:8443/x") != nil {
		t.Fatal("나무 · N2 주소는 허용")
	}
	for _, u := range []string{"http://api.nhplug.com:8443/x", "https://api.nhplug.com.evil.io/x", "https://moapi.nhplg.com/x", "http://127.0.0.1:9/x"} {
		os.Unsetenv("MFROAD_NH_BASE")
		if checkHost(u) == nil {
			t.Errorf("%s 는 막아야 해요", u)
		}
	}
}

func TestMarketHours(t *testing.T) {
	at := func(s string) time.Time { x, _ := time.ParseInLocation("2006-01-02 15:04", s, kst); return x }
	cases := map[string]bool{"2026-10-26 09:30": true, "2026-10-26 07:30": false, "2026-10-26 19:59": true, "2026-10-26 20:30": false, "2026-10-25 11:00": false}
	for s, want := range cases {
		if stockMarketOpen(at(s)) != want {
			t.Errorf("%s → %v", s, !want)
		}
	}
}

func TestNumAndCode(t *testing.T) {
	if num("72,300") != 72300 || num(" +1.5 ") != 1.5 || num(3.0) != 3 || num(nil) != 0 {
		t.Fatal("num")
	}
	if normCode("A005930") != "005930" || normCode("005930") != "005930" || normCode(" 0177N0 ") != "0177N0" {
		t.Fatal("normCode")
	}
	if chosung("삼성전자") != "ㅅㅅㅈㅈ" || !isChosungQuery("ㅅㅅ") || isChosungQuery("삼ㅅ") {
		t.Fatal("chosung")
	}
}

// 처음 켠 직후 검색하면 목록이 준비될 때까지 잠깐 기다려요 (바로 '준비 중' 오류를 내지 않음)
func TestWaitForReady(t *testing.T) {
	var ready atomic.Bool
	go func() { time.Sleep(300 * time.Millisecond); ready.Store(true) }()
	start := time.Now()
	if !waitFor(ready.Load, 3*time.Second) {
		t.Fatal("should become ready")
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Fatal("returned too early")
	}
	if waitFor(func() bool { return false }, 200*time.Millisecond) {
		t.Fatal("should time out")
	}
	if retryWait(1, false) != 5*time.Second || retryWait(1, true) != 2*time.Minute {
		t.Fatal("retry waits")
	}
}
