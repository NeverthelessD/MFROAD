# MFROAD

## ⬇️ 앱 받기

**[MFROAD.zip 받기 (macOS)](https://github.com/NeverthelessD/MFROAD/raw/main/download/MFROAD.zip)**

항상 가장 최신판이에요. zip 안에 앱과 사용 가이드가 함께 들어 있어요.

My Finance ROAD — 국내주식 · 코인 자산 확인 (macOS · Apple Silicon) — made by. Nevertheless_D

종목이나 코인을 검색해서 **어느 계좌에, 몇 개를, 얼마에** 샀는지 넣어 두면 지금 가격으로 평가 금액 · 손익 · 총 자산을 계속 계산해 줘요.

- **국내주식**: 나무증권 PLUG 로 실시간 시세 · 나무증권 계좌의 보유 종목 자동 불러오기 (연결 전에는 전일 종가)
- **코인**: 업비트 원화 시세 (키 필요 없음)
- **같은 종목 · 여러 계좌**: 나무증권 · 한국투자 · 업비트 · 빗썸처럼 계좌마다 따로 넣으면 합쳐서 보여 주고, 줄을 누르면 계좌별로 펼쳐져요
- **검색**: 이름 · 초성(ㅅㅅㅈㅈ) · 종목 코드 · 코인 기호
- **화면**: 맥 설정 따르기(기본) / 라이트 / 다크
- **안전**: 나무증권 키는 맥 키체인에만, 조회만 하고 주문은 하지 않아요
- **자동 업데이트**: 새 버전이 나오면 앱이 알아서 받아요

## 설치

1. 위의 **앱 받기**에서 `MFROAD.zip`을 받아 압축을 풀어요. (안에 앱과 사용 가이드가 들어 있어요)
2. `MFROAD.app`을 **응용 프로그램** 폴더로 옮겨요. (업데이트가 동작하려면 꼭 옮겨 주세요)
3. 처음 열 때 경고가 뜨면 **시스템 설정 → 개인정보 보호 및 보안 → "그래도 열기"**.

사용 가이드: [`docs/MFROAD_사용가이드.png`](docs/MFROAD_사용가이드.png)

자료(보유 내역 · 설정)는 `~/Library/Application Support/MFROAD` 한 폴더에만 저장돼요. 나무증권 키는 키체인(이름: MFROAD)에 있어요.

## 사용한 오픈소스

- 글꼴: [IBM Plex Sans KR](https://github.com/IBM/plex), [Space Grotesk](https://github.com/floriankarsten/space-grotesk) (SIL OFL)
- [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) (BSD) — 종목 목록(CP949) 읽기
- 나무증권 PLUG 사용법은 NH투자증권의 공식 nhplug-sdk 를 참고했어요

## 폴더 구조

```
app/        MFROAD.app 껍데기 (Swift: Dock 앱 · 창 · 엔진 실행) · Info.plist · 아이콘
engine/     Go 엔진 (로컬 서버 · 자산 기록 · 시세 · 나무증권 · 업비트 · 키체인 · 업데이트)
engine/web/ 앱 화면 (HTML · CSS · JS · 글꼴)
scripts/    맥 빌드 · 시험 · 릴리스, 가짜 시세 서버, 아이콘 · 가이드 이미지
releases/   업데이트 파일 (MFROAD-x.y.z.zip · latest.json)
docs/       사용 가이드 이미지와 원본(guide.html)
```

자세한 구조 · 결정 사항 · 진행 상황은 [`PROJECT_NOTES.md`](PROJECT_NOTES.md)에 있어요.
