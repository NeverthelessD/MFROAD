> **이 파일을 읽은 Claude는 작업이 끝날 때마다 이 파일을 업데이트할 것.**
> (구조가 바뀌면 「구조」, 새로 정한 것은 「결정 사항」, 한 일·남은 일은 「진행 사항」에 적고 커밋·푸시까지 할 것)

# MFROAD — 프로젝트 노트

My Finance ROAD. 국내주식 · 코인 자산 확인 앱. macOS (Apple Silicon). made by. Nevertheless_D
사용자는 개발자가 아니에요. 한국어로 쉽게 설명하고, 터미널 사용을 요구하지 말 것. 결과물은 더블클릭으로 쓸 수 있게.

---

## 진행 사항

- **2026-10-29 내려받기용 zip (사용자 요청)**: 저장소 `download/` 폴더에 `MFROAD.zip`(앱 + 사용 가이드)를 올리고, README 맨 위 '앱 받기' 링크(`raw/main/download/MFROAD.zip`)로 바로 받게 했어요. 예전 버전 zip 은 지웠어요 (저장소 `releases/` 에도 업데이트용 최신판만).
작업이 중간에 끊기면 아래 체크리스트에서 **처음으로 체크 안 된 항목부터** 이어서 하면 돼요.

- [x] 0. 요구 정리 · 나무증권 PLUG API 조사 · 디자인 시안 (Design 캔버스 'MFROAD 디자인 시안', 2026-10-26 확정)
- [x] 1. 저장소 뼈대 (이 파일, CLAUDE.md, VERSION, .gitignore)
- [x] 2. 엔진: 포트폴리오(계좌별 매수 기록) · 종목 검색 · 시세 루프 · 나무 PLUG · 업비트 · 키체인 · 지킴이 · 진단 · 업데이트
- [x] 3. 화면: 메인 · 종목 추가/편집 · 설정 · 업데이트 (확정 시안대로)
- [x] 4. 맥 껍데기 · 아이콘 · 빌드/시험 스크립트 · 맥 빌드 서버 워크플로
- [x] 5. 리눅스에서 가짜 시세 서버 + Playwright 로 시험 (go test 6개, 화면 스크린샷을 시안과 비교)
- [x] 6. 맥 빌드 서버 통과(시험 29개) → v1.0.0 릴리스 · 사용 가이드 이미지 · 처음 설치용 zip (2026-10-26)

### 다음에 할 수 있는 일
- [ ] 사용자 맥에서 실제 나무증권 키로 확인 (토큰 발급 · 계좌 목록 · 잔고 가져오기 · 현재가)
- [ ] 실시간 웹소켓(나무 PLUG 27채널 · 업비트) — 지금은 REST 를 빠르게 반복해서 가져옴

### 작업 기록
- 2026-10-26 디자인 확정. 사용자 추가 요청: 같은 종목을 여러 증권사(나무 · 한투 …)에서 다른 평단으로 샀을 수 있음 → 계좌별 매수 기록으로 저장하고 합쳐서 보여 주기.
- 2026-10-26 (작업이 한 번 끊겼다가 이어서) 요약 카드 꼬리표: 여러 계좌면 '계좌 N개 · N종목'.
  첫 맥 시험에서 A4/A5(켜자마자 검색) 실패 → /api/search 가 목록 준비를 최대 12초 기다리고, 목록 받기 실패 시 5·15·45초로 빨리 재시도.
  원인: 빌드 서버 맥이 시험용 Python 가짜 서버에 '로컬 네트워크 허용' 창을 띄워 처음 ~30초 연결이 막힘(실제 사용과 무관). 시험은 목록 준비 뒤 검색하도록.
  → build=ok · smoke=ok (29개 통과) → v1.0.0 릴리스.
- 2026-10-26 v1.0.1: 총 자산 카드에 '평균 단가 기준 N원'(= Σ 수량 × 평균 단가, 시세와 무관 · 탭별) 작게 추가 (사용자 요청).

## 결정 사항

- **GitHub 에는 늘 최신판 zip 1개만**: 업데이트를 내보낼 때마다 `scripts/publish-download.sh` 가 앱 + 가이드 zip 을 `download/` 에 정해진 이름으로 바꿔 넣고 다른 파일은 지워요(release.sh 가 자동으로 불러요. GitHub 릴리스 페이지는 이 작업 환경에서 만들 수 없어 저장소 폴더를 써요). `releases/` 의 업데이트용 zip 도 현재 버전만 남겨요 — 설치된 앱은 latest.json 이 가리키는 zip 만 받고, '되돌리기'는 맥 안의 백업을 써서 예전 zip 이 필요 없어요.
- 이름: MFROAD (My Finance ROAD). 다루는 것: **국내주식 + 코인** (해외주식은 나중에).
- 시세
  - 국내주식: 나무증권 PLUG(REST, `https://api.nhplug.com:8443`). 키가 없으면 종목 마스터의 **전일 종가**로 보여 주고 "나무증권을 연결하면 실시간" 안내.
  - 코인: 업비트 공개 시세(키 필요 없음, 원화 마켓).
  - '실시간'은 지금은 REST 를 짧은 간격으로 반복(코인 1초 · 주식은 초당 4회 한도 안에서 돌아가며). 장이 닫힌 시간(평일 20:10~07:50 · 주말)에는 5분마다.
- **같은 종목 · 여러 계좌**: 보유 내역은 '매수 기록(lot)' = (자산 종류, 종목, 계좌, 수량, 평균 단가). 화면은 종목별로 합쳐서
  (수량 합 · 가중 평균 단가 · 평가 · 손익) 보여 주고, 줄을 누르면 계좌별 기록이 펼쳐짐. 계좌 = 이름(나무증권 · 한국투자 · 업비트 …) + 종류(주식/코인).
- 나무증권 자동 불러오기: `/n2/acctinfo` 의 운영 계좌(acct_type 01·02)마다 `/krstock/inquiry/v1/balance` → 그 계좌의 기록을 통째로 바꿈(직접 고칠 수 없음, '자동' 표시).
  설정의 '보유 종목을 계좌에서 자동으로 불러오기'가 켜져 있으면 켤 때 + 10분마다 + '계좌 다시 불러오기' 버튼.
- 안전: NH 클라이언트는 **읽기 전용 경로만** 부를 수 있게 막아 둠(주문 API 없음). 키는 https 로 허용된 주소(api.nhplug.com · api.n2plug.com)에만.
  App Key/Secret 은 **맥 키체인**에만 저장(`/usr/bin/security`, 비밀값은 명령줄이 아닌 표준 입력으로, 16진수로 넘김). 화면에는 App Key 끝 4자리만.
  접근 토큰은 24시간 유효하고 **새로 받을 때마다 사용자에게 보안 알림**이 가므로 `Support/nh-token.json`(0600)에 저장해 다시 씀.
- 데이터: `~/Library/Application Support/MFROAD/` — portfolio.json(계좌 · 기록), settings.json, history/YYYY-MM-DD.json(오늘 총자산 1분 기록, 7일치),
  cache/(종목 마스터 · 업비트 마켓 목록, 12시간), 기록 파일들(launcher.log · requests.log · engine.log …).
- 업데이트: 이 저장소의 `releases/latest.json` 을 켤 때 + 6시간마다 확인, 자동 설치(기본 켬), 수동 '업데이트 확인', 버전 표시, 이전 버전 되돌리기.
- 화면 모드: 맥 설정 따름(기본) / 라이트 / 다크. 화면 아래 `made by. Nevertheless_D`.
- 사용자에게 보이는 곳에는 GitHub 나 저장소 이름을 쓰지 않음("새 버전이 나오면 앱이 알아서 받아요").
- 커밋 작성자: `Nevertheless_D <NeverthelessD@users.noreply.github.com>`

### 확정 디자인 (2026-10-26)
- 색: 바탕 #F4F3EF / 카드 #FFFFFF / 보조 #F7F6F2 / 글자 #17191E / 흐린 글자 #5C616B / 선 #E3E1DB / 대표색(숲 녹색) #1F6B52 · 옅은 #E5F0EA
  다크: 바탕 #0E1013 / 카드 #16191E / 보조 #1D2127 / 글자 #ECEDEF / 흐린 #9CA2AC / 선 #2A2F37 / 대표색 #5CC79E · 옅은 #173229 (버튼 글자 #0E1013)
  상승 빨강 #D02A3A(다크 #FF6B76) · 하락 파랑 #1F5BD0(다크 #72A6FF), 늘 ▲▼ · +/− 함께. 코인 비율 막대 #A3AAB6(다크 #4B5361).
- 글꼴: 본문 IBM Plex Sans KR, 로고 · 큰 숫자 Space Grotesk(숫자 폭 고정 tabular-nums). 둘 다 SIL OFL, 앱 안에 넣음.
- 아이콘: 숲 녹색 둥근 사각형 + 흰 꺾은선 화살표(위로) + 점점 높아지는 반투명 막대 3개(주가 차트 느낌). 다크용은 어두운 바탕 + 녹색 선.
- 메인: 위 줄(로고 · 전체/국내주식/코인 탭 · 실시간 시각 · 새로고침 · 설정 · 종목 추가) → 총 자산 카드(총액 · 그 아래 작게 '평균 단가 기준' 금액 · 총 손익 · 오늘 · 오늘 그래프 · 주식/코인 비율 막대)
  + 국내주식 · 코인 카드 → 보유 종목 표(종목 · 현재가/전일 대비 · 보유 수량 · 평균 단가 · 평가 금액 · 손익), 방금 바뀐 가격은 잠깐 색 깜빡임.
- 종목 추가: 국내주식/코인 전환 · 검색 · 결과 4개 · (추가) 계좌 고르기 · 보유 수량 · 평균 매수가 · 현재가/예상 평가/예상 손익 · 나무증권 계좌에서 한 번에 불러오기.
- 설정: 나무증권 연결(키 끝자리 · 변경 · 키체인 안내 · 토큰 남은 시간 · 연결 확인 · 자동 불러오기) · 코인(업비트, 키 없음) · 시세 갱신(실시간/5초/30초/1분)
  · 화면(맥 설정 따름/라이트/다크) · 업데이트(버전 · 확인 · 자동) · 진단 정보 보기.

## 구조

```
app/shell/main.swift   MFROAD.app 본체(Swift/AppKit): Dock 아이콘 · 메뉴 · WKWebView 창 · 엔진 실행/종료 · Dock 다시 열기
                       · 업데이트 후 다시 열기(restart-requested → NSWorkspace, MFROAD_RELAUNCHED) · launcher.log
app/Info.plist         LSUIElement 없음, NSPrincipalClass NSApplication, NSAllowsLocalNetworking, 자동/갑작스런 종료 끔
app/AppIcon.*          scripts/icon.py 로 만듦 (svg → png → icns)
engine/main.go         설정 · 라우트 · 127.0.0.1:47931 + 토큰 · lastport · /diag
engine/supervise.go    지킴이: 엔진을 자식으로, '고장'일 때만 같은 주소로 다시 켬(5분에 5번), 종료 신호는 늘 따름, 앱이 사라지면 끝
engine/portfolio.go    계좌(Account) · 매수 기록(Lot) — portfolio.json(0600). 같은 계좌 · 같은 종목은 합쳐서 평균 단가 다시 계산.
                       나무증권 계좌(id "nh-<계좌번호>", source "nh")는 ReplaceNH 로 통째로 바뀜, 직접 고칠 수 없음
engine/market.go       시세 모음(qrev) · 코인/주식 반복 · 나무증권 자동 불러오기(10분) · 총자산 기록(1분) · /api/poll
engine/nh.go           나무증권 PLUG: 토큰(파일 캐시) · 초당 4회 · 읽기 전용 경로만(nhAllowed) · 허용 주소만(checkHost)
                       · 계좌 목록(acct_type 01·02) · 잔고(연속 조회 cts) · 현재가(UNT → KRX → ETF)
engine/secrets.go      키체인(/usr/bin/security, 표준 입력 + 16진수) · 시험용 파일(MFROAD_TEST_SECRETS=file) · Support/test.json(시험 주소)
engine/master.go       종목 마스터 m_new_stock.mst(237바이트 · CP949) 12시간 캐시 · 검색(이름 · 초성 · 코드 · 영문) · 전일 종가
engine/upbit.go        업비트 마켓 목록(12시간) · 현재가(한 번에 여러 개) · 검색
engine/history.go      history/YYYY-MM-DD.json (1분 한 점, 7일)
engine/api.go          /api/search · /api/quote · /api/nh/keys · forget · test · sync
engine/update.go       releases/latest.json → zip · sha256 · version.txt · 백업 · 되돌리기 · restart-requested
engine/web/            index.html · css/app.css(디자인 토큰) · js/app.js(화면 전체) · fonts/(IBM Plex Sans KR 4굵기 · Space Grotesk)
scripts/mock-market.py 가짜 나무증권 + 업비트 + 종목 마스터 (--static 은 시안과 같은 값). 'BADKEY' = 틀린 키. /_stats
scripts/build-mac.sh   (맥) go test → Go + swiftc → 번들 → codesign --deep -s - → zip 2개
scripts/smoke-mac.sh   (맥) 켜기 · 검색 · 키체인 · 나무증권 불러오기 · 여러 계좌 · 시세 · Dock 다시 열기 · 고장 복구(토큰 재사용) · ⌘Q · 종료 버튼 · 강제 종료 · 업데이트 재실행
scripts/ci-mac.sh      빌드 서버 진입점 → ci-results 브랜치
scripts/release.sh     ci-results 가 지금 VERSION/통과인지 확인 → releases/MFROAD-<v>.zip · latest.json · dist/MFROAD.zip
scripts/guide.py       docs/guide.html → docs/MFROAD_사용가이드.png
```
엔진 실행 사슬: MFROAD.app(Swift, Dock) → mfroad-engine(지킴이, MFROAD_SUPERVISOR=1) → mfroad-engine(엔진, MFROAD_CHILD=1).

### 리눅스에서 시험하는 법
```
cd engine && go test ./... && go build -o /tmp/mfroad-engine .
python3 scripts/mock-market.py 47999 --static &        # 가짜 시세
MFROAD_SUPPORT=/tmp/sup MFROAD_TOKEN=t MFROAD_NH_BASE=http://127.0.0.1:47999 MFROAD_UPBIT_BASE=http://127.0.0.1:47999 \
  MFROAD_MASTER_URL=http://127.0.0.1:47999/m_new_stock.mst MFROAD_TEST_SECRETS=file /tmp/mfroad-engine &
# 화면: http://127.0.0.1:47931/?t=t  (Playwright)
```
주의: 프로세스는 pid 파일로 끄기 — `pkill -f mock-market` 은 내 셸 명령줄과도 맞아서 셸이 죽어요.

## 업데이트 내보내는 법 (Claude용)

1. 수정 → `VERSION` 올리기 → 커밋 · 푸시 (app/ · engine/ · VERSION 이 바뀌면 맥 빌드 서버가 자동으로 돎, 5~10분)
2. `git ls-remote origin ci-results` 가 바뀔 때까지 기다린 뒤 결과 확인:
   `git fetch origin +refs/heads/ci-results:refs/remotes/origin/ci-results && git archive origin/ci-results | tar -x -C <임시폴더>`
   → status.json(build/smoke 모두 ok), results.txt, shot-app.png 확인
3. `scripts/release.sh --notes "바뀐 점 1" "바뀐 점 2"` → 커밋 · 푸시. 처음 설치용은 dist/MFROAD.zip.
