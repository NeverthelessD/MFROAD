package main

// 나무증권 PLUG (NH투자증권 REST Open API) — 읽기 전용으로만 써요.
//
//   토큰   POST {auth}/oauth2/token?appkey=…&appsecretkey=…&grant_type=client_credentials&scope=oob (24시간 유효)
//          ⚠️ 새로 받을 때마다 사용자에게 보안 알림이 가요 → Support/nh-token.json(0600)에 저장해 다시 쓰고,
//             만료되었거나 '유효하지 않은 토큰'(401 · IGW40043)일 때만 새로 받아요. 429(너무 자주)에는 새로 받지 않아요.
//   호출   POST {base}{path}  body {"Input_0": {...}}, 헤더 x-client-id · x-client-secret · authorization: Bearer …
//          연속 조회는 응답 헤더 cts · cts_flag 를 다음 요청 헤더로 그대로 보내요.
//   한도   초당 5회 → 초당 4회로 스스로 조절해요.
//
// 안전: nhAllowed 에 적힌 조회용 주소만 부를 수 있어요(주문 API 는 막혀 있어요).
//       키는 https 로 api.nhplug.com · api.n2plug.com 에만 보내요(시험할 때만 MFROAD_NH_BASE 로 이 맥 안 주소 허용).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var nhAllowed = map[string]bool{
	"/n2/acctinfo":                   true, // 계좌 목록
	"/krstock/inquiry/v1/balance":    true, // 국내주식 잔고
	"/krstock/quote/v1/currentPrice": true, // 국내주식 현재가
	"/krstock/quote/v1/etfCurrent":   true, // ETF/ETN 현재가
}

var nhHosts = map[string]bool{"api.nhplug.com": true, "api.n2plug.com": true}

const nhDefaultBase = "https://api.nhplug.com:8443"

type NHAccount struct {
	No   string `json:"no"`
	Type string `json:"type"`
}

type NH struct {
	support string
	mu      sync.Mutex
	appKey  string
	secret  string
	brand   string // nhplug | n2plug

	token    string
	tokenExp time.Time
	issueMu  sync.Mutex      // 토큰은 한 번에 하나만 받아요 (여러 조회가 동시에 시작해도 알림은 한 번)
	plain401 map[string]bool // 새 토큰으로도 401 이 오는 조회 → 토큰 문제가 아님 (다시는 토큰을 바꾸지 않음)
	tokenLog []string        // 토큰을 새로 받은 때 · 이유 (최근 10개, 진단 정보에 보여 줘요)

	state    string // none | ok | error
	err      string
	accounts []NHAccount
	lastOK   time.Time

	syncing  bool
	lastSync time.Time
	syncErr  string
	syncN    int

	rateMu sync.Mutex
	calls  []time.Time
	client *http.Client
}

func NewNH(support string) *NH {
	return &NH{support: support, state: "none", brand: "nhplug", client: &http.Client{Timeout: 12 * time.Second}}
}

func (n *NH) base() string {
	if b := env("NH_BASE"); b != "" && isLocalURL(b) {
		return strings.TrimRight(b, "/")
	}
	if n.brand == "n2plug" {
		return "https://api.n2plug.com:8443"
	}
	return nhDefaultBase
}

// checkHost: 키가 나가는 주소가 허용된 곳인지 마지막으로 한 번 더 확인해요.
func checkHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("나무증권 주소가 올바르지 않아요")
	}
	if u.Scheme == "https" && nhHosts[strings.ToLower(u.Hostname())] {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") && env("NH_BASE") != "" {
		return nil
	}
	return errors.New("허용되지 않은 주소라서 키를 보내지 않았어요")
}

// LoadKeys: 키체인에서 키를 읽어요 (앱을 켤 때)
func (n *NH) LoadKeys() {
	k, err1 := secretGet("nh-appkey")
	s, err2 := secretGet("nh-appsecret")
	b, _ := secretGet("nh-brand")
	n.mu.Lock()
	defer n.mu.Unlock()
	if err1 != nil || err2 != nil {
		n.state, n.err = "error", "키체인에서 나무증권 키를 읽지 못했어요"
		log.Printf("nh: keychain read failed")
		return
	}
	n.appKey, n.secret = k, s
	if b == "n2plug" {
		n.brand = "n2plug"
	}
	if k != "" && s != "" {
		n.state = "ok"
		n.loadToken()
		log.Printf("nh: keys loaded (…%s, %s)", tail4(k), n.brand)
	} else {
		n.state = "none"
	}
}

func (n *NH) Configured() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.appKey != "" && n.secret != ""
}

func tail4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

func (n *NH) Status() map[string]any {
	n.mu.Lock()
	defer n.mu.Unlock()
	left := 0
	if n.token != "" && time.Now().Before(n.tokenExp) {
		left = int(time.Until(n.tokenExp).Seconds())
	}
	var lastSync int64
	if !n.lastSync.IsZero() {
		lastSync = n.lastSync.UnixMilli()
	}
	keyTail := ""
	if n.appKey != "" {
		keyTail = tail4(n.appKey)
	}
	return map[string]any{
		"configured": n.appKey != "" && n.secret != "",
		"state":      n.state,
		"error":      n.err,
		"keyTail":    keyTail,
		"brand":      n.brand,
		"tokenLeft":  left,
		"accounts":   len(n.accounts),
		"syncing":    n.syncing,
		"lastSync":   lastSync,
		"syncError":  n.syncErr,
		"synced":     n.syncN,
		"tokenLog":   append([]string(nil), n.tokenLog...),
	}
}

func (n *NH) DiagLine() string {
	st := n.Status()
	if st["configured"] != true {
		return "연결 안 됨"
	}
	return fmt.Sprintf("상태=%v · 토큰 %v초 남음 · 계좌 %v개 · 마지막 불러오기 %v %v · %v · 토큰 받은 기록: %v",
		st["state"], st["tokenLeft"], st["accounts"], st["lastSync"], st["syncError"], st["error"], st["tokenLog"])
}

func (n *NH) setErr(msg string) {
	n.mu.Lock()
	n.state, n.err = "error", msg
	n.mu.Unlock()
}

func (n *NH) setOK() {
	n.mu.Lock()
	n.state, n.err, n.lastOK = "ok", "", time.Now()
	n.mu.Unlock()
}

// ---------------------------------------------------------------- 토큰 (파일에 저장해 다시 써요)

func (n *NH) tokenScope() string {
	h := sha256.Sum256([]byte(n.appKey + "|" + n.base()))
	return hex.EncodeToString(h[:8])
}

func (n *NH) tokenPath() string { return filepath.Join(n.support, "nh-token.json") }

type tokenFile struct {
	Scope    string   `json:"scope"`
	Token    string   `json:"token"`
	Exp      int64    `json:"exp"`
	Plain401 []string `json:"plain401,omitempty"`
	Log      []string `json:"log,omitempty"`
}

// loadToken: n.mu 를 잡은 채로 불러요
func (n *NH) loadToken() {
	var t tokenFile
	b, err := os.ReadFile(n.tokenPath())
	if err != nil || json.Unmarshal(b, &t) != nil {
		return
	}
	n.tokenLog = t.Log
	if t.Scope != n.tokenScope() {
		return
	}
	n.plain401 = map[string]bool{}
	for _, p := range t.Plain401 {
		n.plain401[p] = true
	}
	if time.Unix(t.Exp, 0).After(time.Now().Add(time.Minute)) {
		n.token, n.tokenExp = t.Token, time.Unix(t.Exp, 0)
	}
}

// saveTokenLocked: n.mu 를 잡은 채로 불러요. 토큰 · 배운 것 · 기록을 파일(0600)에 남겨요.
func (n *NH) saveTokenLocked() {
	t := tokenFile{Scope: n.tokenScope(), Token: n.token, Exp: n.tokenExp.Unix(), Log: n.tokenLog}
	for p := range n.plain401 {
		t.Plain401 = append(t.Plain401, p)
	}
	sort.Strings(t.Plain401)
	b, _ := json.Marshal(t)
	writeFileAtomic(n.tokenPath(), b, 0o600)
}

// issueToken: 키로 새 토큰을 받아요 (key/secret 을 따로 받아 저장 전 시험에도 써요)
func (n *NH) issueToken(base, key, secret string) (string, time.Time, error) {
	u := base + "/oauth2/token"
	if err := checkHost(u); err != nil {
		return "", time.Time{}, err
	}
	q := url.Values{}
	q.Set("appkey", key)
	q.Set("appsecretkey", secret)
	q.Set("grant_type", "client_credentials")
	q.Set("scope", "oob")
	var last string
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequest("POST", u+"?"+q.Encode(), nil)
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		n.throttle()
		resp, err := n.client.Do(req)
		if err != nil {
			last = "나무증권 서버에 연결하지 못했어요. 인터넷 연결을 확인해 주세요"
			time.Sleep(time.Duration(300*(attempt+1)) * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == 200 {
			var d map[string]any
			json.Unmarshal(body, &d)
			tok := str(d["access_token"])
			if tok == "" {
				return "", time.Time{}, errors.New("나무증권이 토큰을 주지 않았어요")
			}
			sec := num(d["expires_in"])
			if sec <= 0 {
				sec = 86400
			}
			log.Printf("nh: new access token issued (expires in %.0fs)", sec)
			return tok, time.Now().Add(time.Duration(sec) * time.Second), nil
		}
		code, msg := nhMessage(body)
		last = friendlyNH(resp.StatusCode, code, msg)
		if strings.Contains(string(body), "IGW40054") && attempt < 2 { // 인증 서버 잠깐 장애
			time.Sleep(time.Duration(400*(attempt+1)) * time.Millisecond)
			continue
		}
		break
	}
	return "", time.Time{}, errors.New(last)
}

// ensureToken: 쓸 수 있는 토큰을 돌려줘요. 저장해 둔 토큰이 살아 있으면 그대로 쓰고(알림 없음),
// 없거나 기한이 지났거나 서버가 방금 거절한 토큰(bad)일 때만 새로 받아요. why 는 진단용 이유.
// 나무증권은 토큰을 새로 받을 때마다 사용자에게 보안 알림을 보내요 → 꼭 필요할 때만, 한 번에 하나만.
func (n *NH) ensureToken(bad, why string) (string, error) {
	n.issueMu.Lock()
	defer n.issueMu.Unlock()
	n.mu.Lock()
	if n.token != "" && n.token != bad && time.Now().Add(30*time.Second).Before(n.tokenExp) {
		t := n.token
		n.mu.Unlock()
		return t, nil
	}
	if why == "" {
		why = "저장된 토큰 없음"
		if n.token != "" {
			why = "토큰 기한(24시간) 지남"
		}
	}
	key, sec, base := n.appKey, n.secret, n.base()
	n.mu.Unlock()
	if key == "" || sec == "" {
		return "", errors.New("나무증권이 연결되어 있지 않아요")
	}
	tok, exp, err := n.issueToken(base, key, sec)
	if err != nil {
		return "", err
	}
	n.mu.Lock()
	n.token, n.tokenExp = tok, exp
	n.noteIssueLocked(why)
	n.saveTokenLocked()
	n.mu.Unlock()
	log.Printf("nh: token reason: %s", why)
	return tok, nil
}

// noteIssueLocked: 토큰을 새로 받은 때와 이유를 남겨요 (n.mu 를 잡은 채로)
func (n *NH) noteIssueLocked(why string) {
	n.tokenLog = append(n.tokenLog, time.Now().Format("01-02 15:04")+" "+why)
	if len(n.tokenLog) > 10 {
		n.tokenLog = n.tokenLog[len(n.tokenLog)-10:]
	}
	diag.line("나무증권 토큰 새로 받음 · " + why)
}

// tokenRejected: 서버 응답이 '토큰이 틀렸다/만료됐다'는 뜻인지. 분명한 신호가 아니면 토큰을 버리지 않아요.
func (n *NH) tokenRejected(path string, status int, raw []byte) (bool, string) {
	code, msg := nhMessage(raw)
	low := strings.ToLower(msg)
	if code == "IGW40043" || strings.Contains(string(raw), "IGW40043") || invalidTokenRe.Match(raw) ||
		((strings.Contains(low, "token") || strings.Contains(msg, "토큰")) && (strings.Contains(msg, "만료") || strings.Contains(low, "expired") || strings.Contains(msg, "유효"))) {
		return true, "서버가 토큰 만료를 알림 (" + path + ")"
	}
	if status == 401 {
		n.mu.Lock()
		known := n.plain401[path]
		n.mu.Unlock()
		if !known {
			return true, fmt.Sprintf("서버가 401 응답 (%s %s)", path, code)
		}
	}
	return false, ""
}

// ---------------------------------------------------------------- 호출

func (n *NH) throttle() {
	for {
		n.rateMu.Lock()
		now := time.Now()
		kept := n.calls[:0]
		for _, t := range n.calls {
			if now.Sub(t) < time.Second {
				kept = append(kept, t)
			}
		}
		n.calls = kept
		if len(n.calls) < 4 {
			n.calls = append(n.calls, now)
			n.rateMu.Unlock()
			return
		}
		wait := time.Second - now.Sub(n.calls[0])
		n.rateMu.Unlock()
		time.Sleep(wait + 5*time.Millisecond)
	}
}

var invalidTokenRe = regexp.MustCompile(`유효하지\s*않은\s*token`)

type nhResult struct {
	Data    map[string]any
	CTS     string
	CTSFlag string
}

func (n *NH) call(path string, input map[string]any, cts, ctsFlag string) (*nhResult, error) {
	if !nhAllowed[path] {
		return nil, errors.New("허용되지 않은 요청이에요")
	}
	n.mu.Lock()
	key, sec, base := n.appKey, n.secret, n.base()
	n.mu.Unlock()
	if key == "" || sec == "" {
		return nil, errors.New("나무증권이 연결되어 있지 않아요")
	}
	if err := checkHost(base + path); err != nil {
		return nil, err
	}
	if input == nil {
		input = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{"Input_0": input})
	bad, why := "", ""
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := n.ensureToken(bad, why)
		if err != nil {
			n.setErr(err.Error())
			return nil, err
		}
		req, _ := http.NewRequest("POST", base+path, strings.NewReader(string(body)))
		req.Header.Set("x-client-id", key)
		req.Header.Set("x-client-secret", sec)
		req.Header.Set("authorization", "Bearer "+tok)
		req.Header.Set("content-type", "application/json; charset=UTF-8")
		if cts != "" {
			req.Header.Set("cts", cts)
		}
		if ctsFlag != "" {
			req.Header.Set("cts_flag", ctsFlag)
		}
		n.throttle()
		resp, err := n.client.Do(req)
		if err != nil {
			msg := "나무증권 서버에 연결하지 못했어요. 인터넷 연결을 확인해 주세요"
			n.setErr(msg)
			return nil, errors.New(msg)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode == 200 {
			var d map[string]any
			if err := json.Unmarshal(raw, &d); err != nil {
				return nil, errors.New("나무증권 응답을 읽지 못했어요")
			}
			n.setOK()
			return &nhResult{Data: d, CTS: strings.TrimSpace(resp.Header.Get("cts")), CTSFlag: strings.TrimSpace(resp.Header.Get("cts_flag"))}, nil
		}
		if attempt == 0 {
			if rej, reason := n.tokenRejected(path, resp.StatusCode, raw); rej {
				bad, why = tok, reason
				continue
			}
		} else if bad != "" && resp.StatusCode == 401 {
			// 방금 받은 새 토큰으로도 401 → 이 조회의 401 은 토큰 문제가 아니에요. 기억해서 다시는 토큰을 바꾸지 않아요.
			n.mu.Lock()
			if n.plain401 == nil {
				n.plain401 = map[string]bool{}
			}
			n.plain401[path] = true
			n.saveTokenLocked()
			n.mu.Unlock()
			log.Printf("nh: %s 401 is not a token problem — remembered", path)
		}
		code, msg := nhMessage(raw)
		e := friendlyNH(resp.StatusCode, code, msg)
		if resp.StatusCode != 429 {
			n.setErr(e)
		}
		return nil, errors.New(e)
	}
	return nil, errors.New("나무증권 인증에 실패했어요")
}

func nhMessage(raw []byte) (string, string) {
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil {
		s := strings.TrimSpace(string(raw))
		if len(s) > 200 {
			s = s[:200]
		}
		return "", s
	}
	code := str(d["rsp_cd"])
	if code == "" {
		code = str(d["error_code"])
	}
	msg := str(d["rsp_msg"])
	if msg == "" {
		msg = str(d["error_description"])
	}
	if msg == "" {
		msg = str(d["error"])
	}
	return code, msg
}

func friendlyNH(status int, code, msg string) string {
	switch {
	case code == "IGW40031" || strings.Contains(msg, "AppKey") || strings.Contains(strings.ToLower(msg), "appkey"):
		return "App Key 또는 App Secret 이 맞지 않아요. 나무증권 PLUG 사이트에서 다시 확인해 주세요"
	case status == 429:
		return "나무증권에 너무 자주 물어봤어요. 잠시 뒤에 다시 시도할게요"
	case status == 401:
		return "나무증권 인증이 만료되었어요. 잠시 뒤 다시 시도할게요"
	}
	if msg != "" {
		return fmt.Sprintf("나무증권: %s (%d)", msg, status)
	}
	return fmt.Sprintf("나무증권 서버 응답 오류 (HTTP %d)", status)
}

// ---------------------------------------------------------------- 키 저장 · 시험

// SaveKeys: 키로 토큰을 받아 보고, 되면 키체인에 저장해요.
func (n *NH) SaveKeys(key, secret, brand string) error {
	key, secret = strings.TrimSpace(key), strings.TrimSpace(secret)
	if key == "" || secret == "" {
		return errors.New("App Key 와 App Secret 을 모두 넣어 주세요")
	}
	if len(key) > 200 || len(secret) > 400 || strings.ContainsAny(key+secret, " \t\r\n") {
		return errors.New("키 모양이 올바르지 않아요. 복사할 때 빈칸이 같이 들어가지 않았는지 확인해 주세요")
	}
	if brand != "n2plug" {
		brand = "nhplug"
	}
	n.mu.Lock()
	prevBrand := n.brand
	n.brand = brand
	base := n.base()
	n.brand = prevBrand
	n.mu.Unlock()
	tok, exp, err := n.issueToken(base, key, secret)
	if err != nil {
		return err
	}
	if err := secretSet("nh-appkey", key); err != nil {
		return err
	}
	if err := secretSet("nh-appsecret", secret); err != nil {
		secretDelete("nh-appkey")
		return err
	}
	secretSet("nh-brand", brand)
	n.mu.Lock()
	n.appKey, n.secret, n.brand = key, secret, brand
	n.token, n.tokenExp = tok, exp
	n.state, n.err = "ok", ""
	n.plain401 = nil
	n.noteIssueLocked("키를 새로 넣음")
	n.saveTokenLocked()
	n.mu.Unlock()
	log.Printf("nh: keys saved to keychain (…%s)", tail4(key))
	return nil
}

func (n *NH) Forget() {
	secretDelete("nh-appkey")
	secretDelete("nh-appsecret")
	secretDelete("nh-brand")
	n.mu.Lock()
	n.appKey, n.secret, n.token, n.tokenExp = "", "", "", time.Time{}
	n.state, n.err, n.accounts, n.syncErr = "none", "", nil, ""
	n.mu.Unlock()
	os.Remove(n.tokenPath())
	log.Printf("nh: keys removed")
}

// ---------------------------------------------------------------- 조회

func (n *NH) Accounts() ([]NHAccount, error) {
	r, err := n.call("/n2/acctinfo", nil, "", "")
	if err != nil {
		return nil, err
	}
	mock := strings.Contains(n.base(), "moapi.")
	var out []NHAccount
	for _, row := range rows(r.Data["Output_0"]) {
		no := str(row["acct_no"])
		t := str(row["acct_type"])
		if no == "" {
			continue
		}
		live := t == "01" || t == "02"
		if t == "03" && !mock || live && mock {
			continue
		}
		out = append(out, NHAccount{No: no, Type: t})
	}
	n.mu.Lock()
	n.accounts = out
	n.mu.Unlock()
	return out, nil
}

// rows: Output 블록이 한 개(객체)로 오거나 여러 개(배열)로 와도 같은 모양으로 바꿔요.
func rows(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		var out []map[string]any
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	}
	return nil
}

type NHHolding struct {
	Code  string
	Name  string
	Qty   float64
	Avg   float64
	Price float64
}

func normCode(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) == 7 && (c[0] == 'A' || c[0] == 'Q' || c[0] == 'J') {
		c = c[1:]
	}
	return c
}

// Balance: 한 계좌의 국내주식 잔고 (연속 조회까지)
func (n *NH) Balance(acctNo string) ([]NHHolding, error) {
	input := map[string]any{
		"act_no": acctNo, "bnc_bse_cd": "5", "ltg_aot_dit_cd": "9",
		"aet_bse": "2", "qut_dit_cd": "UNT", "aly_qut_cd": "2",
	}
	var out []NHHolding
	cts, flag := "", ""
	seen := map[string]bool{}
	for page := 0; page < 30; page++ {
		r, err := n.call("/krstock/inquiry/v1/balance", input, cts, flag)
		if err != nil {
			return nil, err
		}
		for _, row := range rows(r.Data["Output_1"]) {
			code := normCode(str(row["iem_cd"]))
			qty := num(row["itg_bnc_qty"])
			if code == "" || qty <= 0 {
				continue
			}
			buy := num(row["phs_pr"])
			eal := num(row["eal_amt"])
			pls := num(row["eal_pls_amt"])
			avg := buy
			// '매입가격'이 한 주 값인지 전체 금액인지 평가손익과 맞춰 봐서 정해요
			if qty > 1 && eal > 0 {
				unitDiff := abs(eal - qty*buy - pls)
				totalDiff := abs(eal - buy - pls)
				if totalDiff < unitDiff {
					avg = buy / qty
				}
			}
			out = append(out, NHHolding{Code: code, Name: str(row["iem_nm"]), Qty: qty, Avg: avg, Price: num(row["now_pr"])})
		}
		if r.CTS == "" || strings.EqualFold(r.CTSFlag, "N") || seen[r.CTS] {
			break
		}
		if r.CTSFlag == "" {
			cd := str(r.Data["rsp_cd"])
			if cd != "00165" && cd != "00218" {
				break
			}
		}
		seen[r.CTS] = true
		cts, flag = r.CTS, r.CTSFlag
	}
	return out, nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Price: 국내주식 현재가 (통합 → KRX → ETF 순서로 시도)
func (n *NH) Price(code string) (*Quote, error) {
	tries := []struct {
		path string
		in   map[string]any
	}{
		{"/krstock/quote/v1/currentPrice", map[string]any{"market_cd": "UNT", "iem_cd": code}},
		{"/krstock/quote/v1/currentPrice", map[string]any{"market_cd": "KRX", "iem_cd": code}},
		{"/krstock/quote/v1/etfCurrent", map[string]any{"iem_cd": code}},
	}
	var lastErr error
	for _, t := range tries {
		r, err := n.call(t.path, t.in, "", "")
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "너무 자주") || strings.Contains(err.Error(), "연결하지 못했어요") || strings.Contains(err.Error(), "맞지 않아요") {
				return nil, err
			}
			continue
		}
		o := rows(r.Data["Output_0"])
		if len(o) == 0 || num(o[0]["stck_prpr"]) <= 0 {
			continue
		}
		row := o[0]
		price := num(row["stck_prpr"])
		diff := abs(num(row["prdy_vrss"]))
		rate := abs(num(row["prdy_ctrt"]))
		switch str(row["prdy_vrss_sign"]) {
		case "4", "5", "8", "9":
			diff, rate = -diff, -rate
		case "3", "0":
			diff, rate = 0, 0
		}
		return &Quote{Kind: "stock", Code: code, Name: str(row["iem_nm"]), Price: price, Change: diff, Rate: rate, Prev: price - diff, Source: "nh", At: time.Now().UnixMilli()}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("이 종목의 현재가를 받지 못했어요")
	}
	return nil, lastErr
}
