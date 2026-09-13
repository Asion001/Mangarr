# mangarr

A Sonarr-style PVR for manga. mangarr monitors series, detects new chapters on
[Keiyoushi](https://keiyoushi.github.io) (Mihon/Tachiyomi) extension sources,
downloads them as CBZ files with `ComicInfo.xml`, and keeps a complete local
library. Reading happens in apps via [Komga](https://komga.org) or
[Kavita](https://www.kavitareader.com), which mangarr tells to rescan after each import.

Status: early development. See [docs/PLAN.md](docs/PLAN.md) for the design.

## Features (v1)

- Series monitoring with Sonarr semantics: monitor options, wanted/missing, history, blocklist, upgrades
- Everything external is a **module** behind a Go interface:
  - **source** – Suwayomi-Server (runs Keiyoushi extensions, FlareSolverr support); replaceable
  - **metadata** – AniList (more later), merged by priority with provenance and field locks
  - **library** – Komga, Kavita (rescan triggers, per-user read progress)
  - **notify** – Telegram, Discord, ntfy, Gotify, Apprise, Webhook (per-series digests)
  - **upscale** – `mangarr-upscaler` worker (waifu2x / Real-CUGAN / Real-ESRGAN via ncnn + Vulkan)
- Read-based cleanup (off by default): delete chapters every reader finished, keep the last N, grace period, recycle bin
- Page upscaling for small pages (off by default)
- SQLite by default, PostgreSQL optional
- Single static Go binary with an embedded React UI

## Running (development)

```bash
make run          # builds and runs on :8787 with data in ./config
```

Environment variables:

| Variable | Default | Description |
|---|---|---|
| `MANGARR_LISTEN` | `:8787` | HTTP listen address |
| `MANGARR_DATA_DIR` | `./config` | Database, staging, backups, recycle bin |
| `MANGARR_DB` | `sqlite://$DATA_DIR/mangarr.db` | or `postgres://user:pass@host:5432/mangarr?sslmode=disable` |
| `MANGARR_LOG_LEVEL` | `info` | debug, info, warn, error |
| `MANGARR_URL_BASE` | | Serve under a sub path, e.g. `/mangarr` |
| `MANGARR_AUTH_DISABLED` | `false` | Disable login (only behind an auth proxy) |

The API is documented at `/api/docs` (OpenAPI at `/api/openapi.json`); use the
`X-Api-Key` header with the key from Settings → General.
