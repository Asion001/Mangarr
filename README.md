# mangarr

A Sonarr-style PVR for manga. mangarr monitors series, finds new chapters on
[Keiyoushi](https://keiyoushi.github.io) (Mihon/Tachiyomi) extension sources,
downloads them as CBZ files with `ComicInfo.xml`, and keeps a complete,
self-contained local library. You read with the apps you like — Mihon,
Paperback, Tachimanga, Panels, Chunky, KOReader — through
[Komga](https://komga.org) or [Kavita](https://www.kavitareader.com), which
mangarr tells to rescan after every change.

```
 mangarr ──► Suwayomi (runs Keiyoushi extensions) ──► FlareSolverr ──► sites
    │
    └──► /data/manga/<lang>/<Series>/*.cbz  ◄── Komga / Kavita ◄── your apps
```

## Features

- **Sonarr semantics** — monitored series and chapters, monitor options
  (all / future / latest N / from chapter N / none), wanted/missing, queue,
  history, blocklist, upgrades, calendar, health checks, backups.
- **Multiple sources per series** with priorities: if a release fails it is
  blocklisted and the next source is tried; with upgrades on, a better
  source or preferred scanlator replaces the file **in place** (read
  progress in Komga/Kavita survives).
- **Everything external is a module** behind a Go interface — see
  [docs/modules.md](docs/modules.md):
  - `source` — **Suwayomi** (Keiyoushi extensions, FlareSolverr, extension
    manager, per-source settings). Replaceable; identities are portable
    `(sourceId, url)` so another engine can take over existing series.
  - `metadata` — **AniList**; all metadata modules are searched by priority
    and merged field by field with provenance and user locks.
  - `library` — **Komga**, **Kavita** (rescans, path mappings, per-user
    read progress).
  - `notify` — **Telegram, Discord, ntfy, Gotify, Apprise, Webhook**, with
    per-series digests ("One Piece: 3 new chapters (1120–1122)").
  - `upscale` — **mangarr-upscaler** worker (waifu2x / Real-CUGAN /
    Real-ESRGAN via ncnn + Vulkan, iGPU or any GPU machine).
- **Library that works on its own** — flat series folders, stable file names,
  `ComicInfo.xml` (validated against the v2.1 schema), `series.json` for
  Komga, `cover.jpg`. Files are written atomically.
- **Read-based cleanup** (off by default) — syncs each reader's progress from
  Komga/Kavita and deletes chapters everyone finished: keep the last N, grace
  period, ignore readers who never opened a series, `keep` tag, dry run,
  recycle bin, restore.
- **Page upscaling** (off by default) — pages narrower than a threshold are
  upscaled 2×–4× and saved as WebP; existing chapters can be re-processed.
- SQLite by default, **PostgreSQL** optional. Single ~30 MB static binary
  (distroless image) with the web UI embedded. OpenAPI docs at `/api/docs`.

## Quick start (Docker)

See [docker/compose.example.yml](docker/compose.example.yml) and the full
[setup guide](docs/setup.md). In short:

1. Start `mangarr`, `suwayomi` (pinned version, no published port),
   `flaresolverr` and `komga` (library mounted read-only).
2. Open `http://<host>:8787`, create the admin account.
3. **Settings → Media management**: add a root folder (e.g. `/data/manga/en`).
4. **Settings → Source modules**: add *Suwayomi* (`http://suwayomi:4567`,
   enable FlareSolverr `http://flaresolverr:8191`). Keiyoushi is added.
5. **Sources**: install extensions (e.g. MangaDex).
6. **Settings → Metadata**: add *AniList*.
7. **Settings → Library servers**: add *Komga* with an admin API key and a
   path mapping if Komga mounts the library elsewhere.
8. **Settings → Notifications**: add Telegram/ntfy/…
9. **Add series**: pick metadata, pick one or more sources, choose what to
   monitor.

## Images

| Image | Arch | Notes |
|---|---|---|
| `ghcr.io/asion001/mangarr` | amd64, arm64 | distroless, runs as nonroot (use `user:` in compose) |
| `ghcr.io/asion001/mangarr-upscaler` | amd64 | Mesa Vulkan (Intel/AMD via `/dev/dri`, lavapipe CPU fallback), cwebp |

## Configuration

| Variable | Default | Description |
|---|---|---|
| `MANGARR_LISTEN` | `:8787` | HTTP listen address |
| `MANGARR_DATA_DIR` | `./config` (`/config` in Docker) | database, staging, backups, recycle bin, caches |
| `MANGARR_DB` | `sqlite://$DATA_DIR/mangarr.db` | or `postgres://user:pass@host:5432/mangarr?sslmode=disable` |
| `MANGARR_LOG_LEVEL` | `info` | debug, info, warn, error |
| `MANGARR_URL_BASE` | | serve under a sub path, e.g. `/mangarr` |
| `MANGARR_AUTH_DISABLED` | `false` | disable login (only behind an auth proxy) |

Everything else is configured in the UI, or pinned with environment
variables: every settings field (`MANGARR_DOWNLOADS_MAX_CONCURRENT=2`), the API
key (`MANGARR_API_KEY`), root folders (`MANGARR_ROOT_FOLDERS`) and whole module
instances (`MANGARR_MODULE_KOMGA_IMPL=library/komga`,
`MANGARR_MODULE_KOMGA_URL=…`). Pinned values show a lock in the UI. See
[docs/configuration.md](docs/configuration.md) or run `mangarr env`.

The API accepts the `X-Api-Key` header (Settings → General).

## Development

```bash
make build        # Go binary (UI embedded from web/dist)
make web          # build the UI (uses Docker if npm is not installed)
make web-types    # regenerate web/src/api/schema.d.ts from the OpenAPI document
make test         # unit + app tests (SQLite)
make test-pg      # also against Postgres (MANGARR_TEST_POSTGRES)
scripts/integration.sh   # real Suwayomi + upscaler + Komga in Docker
scripts/upscale-bench.sh http://worker:8788 <token> chapter.cbz   # pick upscaler models for your GPU
```

Design notes and the implementation plan: [docs/PLAN.md](docs/PLAN.md).
