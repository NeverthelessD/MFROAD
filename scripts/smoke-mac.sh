#!/bin/bash
# MFROAD 실제 맥 시험 (맥 빌드 서버에서 dist/MFROAD.app 을 /Applications 에 깔고 여러 상황을 시험해요)
# 시세는 가짜 시세 서버(scripts/mock-market.py)로 받아요 — 진짜 나무증권 · 업비트에는 연결하지 않아요.
# 나무증권 키는 진짜 키체인에 저장되는지 확인하고, 끝나면 지워요.
# 결과: $OUT/results.txt (PASS/FAIL 한 줄씩), 스크린샷, 기록 파일
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OUT:-$ROOT/ci-out}"
mkdir -p "$OUT"
APPDIR="/Applications/MFROAD.app"
SUP="$HOME/Library/Application Support/MFROAD"
RES="$OUT/results.txt"
MOCK=47999
: > "$RES"

pass() { echo "PASS  $*" | tee -a "$RES"; }
fail() { echo "FAIL  $*" | tee -a "$RES"; }
check() { local name="$1"; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }

app_pid() { pgrep -x MFROAD | head -1; }
engines() { pgrep -x mfroad-engine | tr '\n' ' '; }
tok() { cat "$SUP/token" 2>/dev/null; }
port() { cat "$SUP/port" 2>/dev/null; }
api() { curl -fs -m 15 -H "x-token: $(tok)" "http://127.0.0.1:$(port)$1"; }
post() { curl -fs -m 20 -X POST -H "x-token: $(tok)" -H 'content-type: application/json' --data "$2" "http://127.0.0.1:$(port)$1"; }
ping_ok() { api /api/ping >/dev/null 2>&1; }
wait_ready() { for _ in $(seq 1 60); do [ -s "$SUP/port" ] && ping_ok && return 0; sleep 0.5; done; return 1; }
wait_gone() { for _ in $(seq 1 30); do [ -z "$(app_pid)" ] && [ -z "$(engines)" ] && return 0; sleep 0.5; done; return 1; }
snap() { echo "--- $1"; ps -axo pid,ppid,stat,comm | grep -E "MFROAD|mfroad-engine" | grep -v grep; } >> "$OUT/procs.txt"
launch() { open "$APPDIR"; wait_ready; }
tokens_issued() { curl -fs "http://127.0.0.1:$MOCK/_stats" | python3 -c "import json,sys; print(json.load(sys.stdin)['tokens'])"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

echo "▶ 준비: 가짜 시세 서버 · 앱 설치 · 설정"
python3 "$ROOT/scripts/mock-market.py" $MOCK --static > "$OUT/mock.log" 2>&1 &
MOCKPID=$!
sleep 1
rm -rf "$APPDIR"; ditto "$ROOT/dist/MFROAD.app" "$APPDIR"
rm -rf "$SUP"; mkdir -p "$SUP"
printf '{"nh_base":"http://127.0.0.1:%s","upbit_base":"http://127.0.0.1:%s","master_url":"http://127.0.0.1:%s/m_new_stock.mst"}' $MOCK $MOCK $MOCK > "$SUP/test.json"
printf '{"appearance":"light","refresh":"live","autoUpdate":false,"autoImport":true,"ui":{"hideNHTip":true}}' > "$SUP/settings.json"
# 키체인: 빌드 서버의 기본 키체인에 쓸 수 없으면 시험용 키체인을 만들어 기본으로 써요
security delete-generic-password -s MFROAD -a nh-appkey >/dev/null 2>&1
security delete-generic-password -s MFROAD -a nh-appsecret >/dev/null 2>&1
if ! security add-generic-password -U -s MFROAD-probe -a probe -w probe >/dev/null 2>&1; then
  echo "기본 키체인을 쓸 수 없어서 시험용 키체인을 만들어요"
  security create-keychain -p smoke "$HOME/mfroad-smoke.keychain-db"
  security unlock-keychain -p smoke "$HOME/mfroad-smoke.keychain-db"
  security set-keychain-settings "$HOME/mfroad-smoke.keychain-db"
  security list-keychains -d user -s "$HOME/mfroad-smoke.keychain-db"
  security default-keychain -s "$HOME/mfroad-smoke.keychain-db"
fi
security delete-generic-password -s MFROAD-probe -a probe >/dev/null 2>&1
security default-keychain > "$OUT/keychain.txt" 2>&1

echo "▶ A. 켜기 · 화면 · 검색"
launch; check "A1 앱을 열면 엔진이 준비됨" ping_ok
check "A2 엔진이 Dock 앱 방식으로 켜짐(shell=native)" bash -c "curl -fs -H 'x-token: $(tok)' http://127.0.0.1:$(port)/api/ping | grep -q '\"shell\":\"native\"'"
check "A3 주소가 47931" test "$(port)" = "47931"
sleep 2
N=$(api "/api/search?kind=stock&q=%EC%82%BC%EC%84%B1" | json "len(d['items'])" 2>/dev/null)
check "A4 국내주식 검색 '삼성' (찾은 수: ${N:-?})" test "${N:-0}" -ge 4
C=$(api "/api/search?kind=coin&q=btc" | json "d['items'][0]['code']" 2>/dev/null)
check "A5 코인 검색 'btc' → ${C:-?}" test "$C" = "BTC"
APID=$(app_pid); snap "A 실행 중"
SUPPID=$(ps -axo pid=,ppid=,comm= | awk -v a="$APID" '$2==a && /mfroad-engine/ {print $1}' | head -1)
CHILD=$(ps -axo pid=,ppid=,comm= | awk -v s="$SUPPID" '$2==s && /mfroad-engine/ {print $1}' | head -1)
check "A6 엔진이 MFROAD 앱의 자식 (앱 $APID → 지킴이 ${SUPPID:-없음} → 엔진 ${CHILD:-없음})" test -n "$SUPPID" -a -n "$CHILD"

echo "▶ K. 나무증권 키 → 키체인 · 계좌 불러오기"
R=$(post /api/nh/keys '{"appKey":"BADKEY","appSecret":"x"}' 2>&1);
check "K1 틀린 키는 저장하지 않음" bash -c "! security find-generic-password -s MFROAD -a nh-appkey >/dev/null 2>&1"
R=$(post /api/nh/keys '{"appKey":"PSsmokeTestKey00a91c","appSecret":"smoke+Secret/Value=="}')
echo "$R" > "$OUT/nh-keys.json"
check "K2 맞는 키를 저장하고 연결됨" bash -c "echo '$R' | grep -q '\"ok\":true'"
check "K3 키가 맥 키체인에 들어감" bash -c "[ \"\$(security find-generic-password -s MFROAD -a nh-appsecret -w 2>/dev/null)\" = 'smoke+Secret/Value==' ]"
check "K4 키가 파일로는 남지 않음" bash -c "! grep -rq 'smoke+Secret' \"$SUP\""
for _ in $(seq 1 20); do [ "$(api /api/portfolio | json "len([l for l in d['lots'] if l.get('source')=='nh'])" 2>/dev/null)" = "3" ] && break; sleep 0.5; done
NHN=$(api /api/portfolio | json "len([l for l in d['lots'] if l.get('source')=='nh'])" 2>/dev/null)
check "K5 나무증권 계좌에서 3종목을 불러옴 (연속 조회 포함, 불러온 수: ${NHN:-?})" test "${NHN:-0}" = "3"

echo "▶ L. 직접 넣기 (같은 종목 · 다른 계좌)"
ACC=$(post /api/account/save '{"name":"한국투자","kind":"stock"}' | json "d['account']['id']")
CAC=$(post /api/account/save '{"name":"업비트","kind":"coin"}' | json "d['account']['id']")
post /api/lot/save "{\"kind\":\"stock\",\"code\":\"005930\",\"account\":\"$ACC\",\"qty\":30,\"avg\":70000}" >/dev/null
M=$(post /api/lot/save "{\"kind\":\"stock\",\"code\":\"005930\",\"account\":\"$ACC\",\"qty\":10,\"avg\":60000}" | json "(d['merged'], d['lot']['qty'], d['lot']['avg'])")
check "L1 같은 계좌에 더 사면 합쳐서 평균 단가 계산 ($M)" test "$M" = "(True, 40, 67500)"
post /api/lot/save "{\"kind\":\"stock\",\"code\":\"000660\",\"account\":\"$ACC\",\"qty\":15,\"avg\":182000}" >/dev/null
post /api/lot/save "{\"kind\":\"coin\",\"code\":\"BTC\",\"account\":\"$CAC\",\"qty\":0.045,\"avg\":88000000}" >/dev/null
post /api/lot/save "{\"kind\":\"coin\",\"code\":\"ETH\",\"account\":\"$CAC\",\"qty\":1.2,\"avg\":4850000}" >/dev/null
post /api/lot/save "{\"kind\":\"coin\",\"code\":\"XRP\",\"account\":\"$CAC\",\"qty\":800,\"avg\":3120}" >/dev/null
SAM=$(api /api/portfolio | json "sorted(l['account'][:3] for l in d['lots'] if l['code']=='005930')")
check "L2 삼성전자가 두 계좌에 따로 있음 ($SAM)" bash -c "echo \"$SAM\" | grep -q \"'a\" && echo \"$SAM\" | grep -q \"'nh-\""
sleep 4
Q=$(api "/api/poll" | json "len(d.get('quotes',{}))")
check "L3 보유 7종목 시세를 모두 받음 (${Q:-0}개)" test "${Q:-0}" = "7"
sleep 3
screencapture -x "$OUT/shot-app.png"
check "L4 화면이 앱 창(WKWebView)에서 열림" grep -q "MFROADShell" "$SUP/requests.log"
T1=$(tokens_issued)

echo "▶ B. Dock 아이콘 다시 누르기(reopen)"
osascript -e 'tell application "MFROAD" to reopen' >/dev/null 2>&1; sleep 1
check "B1 다시 열기 뒤에도 앱·엔진 그대로" test "$(app_pid)" = "$APID"

echo "▶ C. 엔진이 고장으로 꺼지면 지킴이가 다시 켬"
kill -SEGV "$CHILD" 2>/dev/null; sleep 4
check "C1 엔진 다시 켜짐" wait_ready
check "C2 앱은 그대로" test "$(app_pid)" = "$APID"
sleep 3
check "C3 다시 켜져도 나무증권 토큰을 새로 받지 않음 (발급 $T1 → $(tokens_issued))" test "$(tokens_issued)" = "$T1"

echo "▶ D. 앱 종료(⌘Q) → 엔진까지 모두 꺼짐"
osascript -e 'tell application "MFROAD" to quit' >/dev/null 2>&1
check "D1 앱·엔진 모두 꺼짐" wait_gone
snap "D 종료 뒤"

echo "▶ K'. 다시 열면 키체인에서 키를 읽어 그대로 연결됨"
launch; sleep 3
check "K6 다시 연 뒤에도 나무증권 연결됨" bash -c "curl -fs -H 'x-token: $(tok)' http://127.0.0.1:$(port)/api/poll | grep -q '\"configured\":true'"
check "K7 토큰은 저장해 둔 것을 씀 (발급 $(tokens_issued)번)" test "$(tokens_issued)" = "$T1"
check "K8 주문 API 는 한 번도 부르지 않음" bash -c "! curl -fs http://127.0.0.1:$MOCK/_stats | grep -q ORDER_CALLED"
sleep 4
screencapture -x "$OUT/shot-relaunch.png"

echo "▶ E. 화면의 '종료'(= /api/quit) → 앱도 꺼짐"
curl -fs -X POST -H "x-token: $(tok)" "http://127.0.0.1:$(port)/api/quit" >/dev/null
check "E1 앱·엔진 모두 꺼짐" wait_gone

echo "▶ F. 앱이 강제로 꺼지면 엔진도 스스로 꺼짐"
launch; kill -9 "$(app_pid)"; sleep 1
check "F1 남은 엔진 없음" bash -c 'for i in $(seq 1 20); do [ -z "$(pgrep -x mfroad-engine)" ] && exit 0; sleep 0.5; done; exit 1'

echo "▶ G. 업데이트 뒤 다시 열기 (/api/update/restart)"
launch; OLD=$(app_pid)
curl -fs -X POST -H "x-token: $(tok)" "http://127.0.0.1:$(port)/api/update/restart" >/dev/null
sleep 3
check "G1 새로 열린 앱이 엔진을 켬" wait_ready
NEW=$(app_pid)
check "G2 앱이 새로 열림 ($OLD → ${NEW:-없음})" test -n "$NEW" -a "$NEW" != "$OLD"
check "G3 앱 하나만 떠 있음 ($(pgrep -x MFROAD | wc -l | tr -d ' ')개)" test "$(pgrep -x MFROAD | wc -l | tr -d ' ')" = "1"
osascript -e 'tell application "MFROAD" to quit' >/dev/null 2>&1
check "G4 끝나면 모두 꺼짐" wait_gone

echo "▶ 기록 모으고 정리"
for f in launcher.log requests.log engine.log engine.prev.log engine.out engine.prev.out settings.json portfolio.json; do
  [ -f "$SUP/$f" ] && cp "$SUP/$f" "$OUT/$f"
done
security delete-generic-password -s MFROAD -a nh-appkey >/dev/null 2>&1
security delete-generic-password -s MFROAD -a nh-appsecret >/dev/null 2>&1
security delete-generic-password -s MFROAD -a nh-brand >/dev/null 2>&1
curl -fs "http://127.0.0.1:$MOCK/_stats" > "$OUT/mock-stats.json"
kill "$MOCKPID" 2>/dev/null
sw_vers > "$OUT/macos.txt"
echo
echo "== 결과 =="; cat "$RES"
! grep -q '^FAIL' "$RES"
