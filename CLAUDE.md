# MFROAD — notes for Claude

**Read `PROJECT_NOTES.md` first** (structure, decisions, progress checklist) and update it at the end of every task.
MFROAD (My Finance ROAD) is a personal macOS (Apple Silicon) asset tracker for **Nevertheless_D**, who is not a programmer:
domestic stocks (NH 나무증권 PLUG Open API) and coins (Upbit public quotes), with the same stock held in several accounts at different average prices.
Talk to them in Korean, in plain words. Never ask them to use Terminal. Never mention GitHub (or the repo name) in anything the user sees.

It follows the ROAD app pattern (see the `road-mac-app` skill and the MSROAD repo): a real Dock-visible Mac app
(Swift shell `app/shell/main.swift` + WKWebView) whose Go engine runs as its child only while the app is open
(app → supervisor → engine, 127.0.0.1 + token, `lastport`, `/api/ping`·`/api/poll` include `"shell"`), self-update from `releases/latest.json`.
Never reintroduce hidden helpers, nohup engines or SIGTERM-ignoring (macOS 27 Background App Activity).

The app can only be built on a Mac: pushing changes under app/, engine/ or VERSION triggers `.github/workflows/build-mac.yml`, which builds,
ad-hoc signs, runs real-Mac smoke tests and force-pushes results to the `ci-results` branch (the session proxy blocks the Actions API,
so read results via git). Release with `scripts/release.sh`. Linux testing: `scripts/mock-market.py` (fake NH + Upbit) — see PROJECT_NOTES.md.

Safety rules for the NH client (`engine/nh.go`): only the read-only paths in `nhAllowed` may ever be called (no order APIs),
credentials go only to the allowed hosts over https, the access token is cached on disk (each new token sends the user a security alert),
and the App Key/Secret live only in the macOS Keychain (`engine/secrets.go`, via `/usr/bin/security`). Never log them.
`signal.Ignore` can't be undone with `signal.Reset`. In tests use `pkill -x`, never `pkill -f mfroad-engine`.
Commit author: `Nevertheless_D <NeverthelessD@users.noreply.github.com>`.
