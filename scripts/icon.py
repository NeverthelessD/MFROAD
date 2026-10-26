#!/usr/bin/env python3
"""MFROAD 앱 아이콘 만들기 (확정 디자인: 숲 녹색 둥근 사각형 + 흰 꺾은선 화살표 + 반투명 막대 3개)
app/AppIcon.svg → app/AppIcon.png (1024) · app/AppIcon.icns (16~1024, PNG 조각)
Playwright + Chromium 필요 · python3 scripts/icon.py"""
import asyncio, os, struct
from playwright.async_api import async_playwright

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def svg(size_hint=1024):
    # 작은 크기에서는 선을 조금 굵게 (디자인 시안의 '작은 크기'와 같음)
    sw = 2.2 if size_hint >= 64 else 2.6
    s = 24  # 24 단위 그림을 24배 → 막대 · 화살표가 아이콘의 절반쯤
    tx, ty = 512 - 11.5 * s, 512 - 12 * s
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">
  <defs>
    <filter id="sh" x="-20%" y="-20%" width="140%" height="140%"><feDropShadow dx="0" dy="10" stdDeviation="14" flood-color="#000" flood-opacity="0.22"/></filter>
    <linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#23775B"/><stop offset="1" stop-color="#1B5F49"/></linearGradient>
  </defs>
  <rect x="100" y="100" width="824" height="824" rx="185" fill="url(#g)" filter="url(#sh)"/>
  <g transform="translate({tx} {ty}) scale({s})" fill="none">
    <rect x="4" y="15" width="3" height="5" rx="0.8" fill="#FFFFFF" fill-opacity="0.45"/>
    <rect x="10.5" y="12.5" width="3" height="7.5" rx="0.8" fill="#FFFFFF" fill-opacity="0.45"/>
    <rect x="17" y="10" width="3" height="10" rx="0.8" fill="#FFFFFF" fill-opacity="0.45"/>
    <path d="M3 13 L8.5 8.5 L12.5 11 L20 4" stroke="#FFFFFF" stroke-width="{sw}" stroke-linecap="round" stroke-linejoin="round"/>
    <path d="M15 4 H20 V9" stroke="#FFFFFF" stroke-width="{sw}" stroke-linecap="round" stroke-linejoin="round"/>
  </g>
</svg>'''


# icns 조각 이름 → 픽셀 크기 (모두 PNG)
CHUNKS = [("icp4", 16), ("icp5", 32), ("icp6", 64), ("ic07", 128), ("ic08", 256), ("ic09", 512), ("ic10", 1024),
          ("ic11", 32), ("ic12", 64), ("ic13", 256), ("ic14", 512)]


async def main():
    open(os.path.join(ROOT, "app", "AppIcon.svg"), "w").write(svg())
    pngs = {}
    async with async_playwright() as p:
        b = await p.chromium.launch()
        for size in sorted({s for _, s in CHUNKS}):
            pg = await b.new_page(viewport={"width": size, "height": size}, device_scale_factor=1)
            inner = svg(size).replace('width="1024" height="1024"', f'width="{size}" height="{size}"', 1)
            await pg.set_content(f'<html><body style="margin:0;background:transparent">{inner}</body></html>')
            await pg.wait_for_timeout(150)
            pngs[size] = await pg.screenshot(omit_background=True)
            await pg.close()
        await b.close()
    open(os.path.join(ROOT, "app", "AppIcon.png"), "wb").write(pngs[1024])
    body = b"".join(t.encode() + struct.pack(">I", len(pngs[s]) + 8) + pngs[s] for t, s in CHUNKS)
    open(os.path.join(ROOT, "app", "AppIcon.icns"), "wb").write(b"icns" + struct.pack(">I", len(body) + 8) + body)
    print("✔ app/AppIcon.svg · AppIcon.png · AppIcon.icns")


asyncio.run(main())
