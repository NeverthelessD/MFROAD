#!/usr/bin/env python3
"""MFROAD 시험용 가짜 시세 서버 — 나무증권 PLUG(토큰 · 계좌 · 잔고 · 현재가) + 업비트(마켓 · 현재가) + 종목 마스터.

usage: mock-market.py [port] [--static] [--slow-nh]
  --static : 가격을 움직이지 않음 (스크린샷용 · 디자인 시안과 같은 값)
엔진에 붙이기: MFROAD_NH_BASE=http://127.0.0.1:<port> MFROAD_UPBIT_BASE=… MFROAD_MASTER_URL=http://127.0.0.1:<port>/m_new_stock.mst
App Key 'BADKEY' 는 틀린 키로 처리해요. GET /_stats 로 토큰 발급 수 · 호출 수를 볼 수 있어요.
"""
import json, random, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

PORT = int(next((a for a in sys.argv[1:] if a.isdigit()), "47999"))
STATIC = "--static" in sys.argv
lock = threading.Lock()
stats = {"tokens": 0, "calls": {}, "bad_auth": 0}

# 종목: 코드 → (이름, 시장코드, ETF?, 기준가, 전일 대비 %)
STOCKS = {
    "005930": ("삼성전자", "1", False, 72300, 1.83),
    "005935": ("삼성전자우", "1", False, 58900, 0.68),
    "006400": ("삼성SDI", "1", False, 312000, -1.27),
    "207940": ("삼성바이오로직스", "1", False, 1024000, 0.39),
    "000660": ("SK하이닉스", "1", False, 176500, -0.85),
    "035420": ("NAVER", "1", False, 189200, 2.10),
    "035720": ("카카오", "1", False, 41250, -0.48),
    "069500": ("KODEX 200", "1", True, 36050, 0.42),
    "247540": ("에코프로비엠", "4", False, 118300, 3.05),
}
COINS = {
    "BTC": ("비트코인", "Bitcoin", 101250000, 1.27),
    "ETH": ("이더리움", "Ethereum", 4412000, -2.04),
    "XRP": ("리플", "XRP", 3415, 3.11),
    "SOL": ("솔라나", "Solana", 262300, 0.85),
    "DOGE": ("도지코인", "Dogecoin", 312, -1.10),
    "BCH": ("비트코인캐시", "Bitcoin Cash", 812400, 0.20),
    "BSV": ("비트코인에스브이", "Bitcoin SV", 72800, -0.40),
}
cur = {k: float(v[3]) for k, v in STOCKS.items()}
ccur = {k: float(v[2]) for k, v in COINS.items()}
prev = {k: v[3] / (1 + v[4] / 100) for k, v in STOCKS.items()}
cprev = {k: v[2] / (1 + v[3] / 100) for k, v in COINS.items()}

# 나무증권 계좌 (01 운영 · 03 모의) 와 잔고
ACCOUNTS = [{"acct_no": "20012345678", "acct_type": "01"}, {"acct_no": "20099990000", "acct_type": "03"}]
BALANCE = {"20012345678": [("005930", 120, 61500), ("035420", 20, 168000), ("069500", 50, 34200)]}


def tick(price, prev_close, is_coin=False):
    if STATIC:
        return price
    step = price * random.uniform(-0.0012, 0.0012)
    p = max(1.0, price + step)
    return round(p) if (not is_coin or p >= 100) else round(p, 2)


def master_bytes():
    recs = []
    def rec(code, name, mkt, etf, prevp):
        r = bytearray(b" " * 237)
        r[0:6] = code.encode()
        r[6:7] = mkt.encode()
        nm = name.encode("cp949")[:41]
        r[7:7 + len(nm)] = nm
        en = name.encode("ascii", "ignore")[:41] or b"X"
        r[48:48 + len(en)] = en
        r[152:159] = f"{int(prevp):07d}".encode()
        r[165:166] = b"8" if etf else b"0"
        r[236:237] = b"\n"
        return bytes(r)
    for code, (name, mkt, etf, base, chg) in STOCKS.items():
        recs.append(rec(code, ("*" if code == "005930" else "") + name, mkt, etf, round(base / (1 + chg / 100))))
    for i in range(130):  # 엔진은 100종목 아래면 망가진 파일로 봐요
        recs.append(rec(f"9{i:05d}", f"시험종목{i}", "1", False, 1000 + i))
    return b"".join(recs)


MASTER = master_bytes()


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, code, obj, headers=None):
        b = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json; charset=utf-8")
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("content-length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def count(self, p):
        with lock:
            stats["calls"][p] = stats["calls"].get(p, 0) + 1

    def do_GET(self):
        u = urlparse(self.path)
        q = parse_qs(u.query)
        self.count(u.path)
        if u.path == "/_stats":
            return self.send(200, stats)
        if u.path == "/m_new_stock.mst":
            self.send_response(200)
            self.send_header("content-length", str(len(MASTER)))
            self.end_headers()
            self.wfile.write(MASTER)
            return
        if u.path == "/v1/market/all":
            return self.send(200, [{"market": "KRW-" + k, "korean_name": v[0], "english_name": v[1]} for k, v in COINS.items()] +
                             [{"market": "BTC-ETH", "korean_name": "이더리움", "english_name": "Ethereum"}])
        if u.path == "/v1/ticker":
            ms = (q.get("markets") or [""])[0].split(",")
            out = []
            for m in ms:
                c = m.replace("KRW-", "")
                if c not in COINS:
                    return self.send(404, {"error": {"name": "404", "message": "Code not found"}})
                with lock:
                    ccur[c] = tick(ccur[c], cprev[c], True)
                    p = ccur[c]
                out.append({"market": m, "trade_price": p, "prev_closing_price": cprev[c], "signed_change_price": p - cprev[c],
                            "signed_change_rate": (p - cprev[c]) / cprev[c], "timestamp": int(time.time() * 1000)})
            return self.send(200, out)
        self.send(404, {"error": "not found"})

    def do_POST(self):
        u = urlparse(self.path)
        q = parse_qs(u.query)
        n = int(self.headers.get("content-length") or 0)
        body = json.loads(self.rfile.read(n) or b"{}") if n else {}
        self.count(u.path)
        if u.path == "/oauth2/token":
            if (q.get("appkey") or [""])[0] == "BADKEY":
                return self.send(403, {"error_code": "IGW40031", "error_description": "유효하지 않은 AppKey 입니다."})
            with lock:
                stats["tokens"] += 1
                t = stats["tokens"]
            return self.send(200, {"access_token": f"tok-{t}", "token_type": "Bearer", "expires_in": 86400})
        auth = self.headers.get("authorization", "")
        if not auth.startswith("Bearer tok-") or not self.headers.get("x-client-id") or not self.headers.get("x-client-secret"):
            stats["bad_auth"] += 1
            return self.send(401, {"error_code": "IGW40043", "error_description": "유효하지 않은 token 입니다."})
        inp = body.get("Input_0", {})
        if u.path == "/n2/acctinfo":
            return self.send(200, {"rsp_cd": "00000", "rsp_msg": "정상", "Output_0": ACCOUNTS})
        if u.path == "/krstock/inquiry/v1/balance":
            rows = []
            for code, qty, avg in BALANCE.get(inp.get("act_no"), []):
                p = cur[code]
                rows.append({"iem_cd": "A" + code, "iem_nm": STOCKS[code][0], "itg_bnc_qty": qty, "phs_pr": avg, "now_pr": p,
                             "eal_amt": qty * p, "eal_pls_amt": qty * (p - avg)})
            # 연속 조회 시험: 첫 장 2개, 다음 장 나머지
            if self.headers.get("cts") == "PAGE2":
                return self.send(200, {"rsp_cd": "00000", "Output_0": {"dca": 0}, "Output_1": rows[2:]}, {"cts": "", "cts_flag": "N"})
            return self.send(200, {"rsp_cd": "00000", "Output_0": {"dca": 1000000}, "Output_1": rows[:2]},
                             {"cts": "PAGE2", "cts_flag": "Y"} if len(rows) > 2 else {"cts_flag": "N"})
        if u.path in ("/krstock/quote/v1/currentPrice", "/krstock/quote/v1/etfCurrent"):
            code = inp.get("iem_cd", "")
            if code not in STOCKS:
                return self.send(200, {"rsp_cd": "00001", "rsp_msg": "조회 내역이 없습니다."})
            with lock:
                cur[code] = tick(cur[code], prev[code])
                p = cur[code]
            d = p - prev[code]
            sign = "2" if d > 0 else "5" if d < 0 else "3"
            return self.send(200, {"rsp_cd": "00000", "Output_0": {"iem_cd": code, "iem_nm": STOCKS[code][0], "stck_prpr": int(p),
                                                                "prdy_vrss_sign": sign, "prdy_vrss": int(round(abs(d))),
                                                                "prdy_ctrt": round(abs(d) / prev[code] * 100, 2)}})
        if "/order/" in u.path:
            stats["ORDER_CALLED"] = True
        self.send(404, {"error": "not found"})


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
    print(f"mock market on 127.0.0.1:{PORT} static={STATIC}", flush=True)
    srv.serve_forever()
