#!/usr/bin/env python3
"""docs/guide.html → docs/MFROAD_사용가이드.png (Playwright + Chromium 필요) · python3 scripts/guide.py"""
import asyncio, os
from playwright.async_api import async_playwright

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


async def main():
    async with async_playwright() as p:
        b = await p.chromium.launch()
        pg = await b.new_page(viewport={"width": 1200, "height": 800}, device_scale_factor=2)
        await pg.goto("file://" + os.path.join(ROOT, "docs", "guide.html"))
        await pg.wait_for_timeout(800)
        await pg.screenshot(path=os.path.join(ROOT, "docs", "MFROAD_사용가이드.png"), full_page=True)
        await b.close()
    print("✔ docs/MFROAD_사용가이드.png")


asyncio.run(main())
