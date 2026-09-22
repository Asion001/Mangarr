# Architecture

How mangarr is put together, for anyone changing it. What it does for users
is in the [README](../README.md) and the [setup guide](setup.md); how to write
a module is in [modules.md](modules.md). Planned work and bugs live in
[GitHub issues](https://github.com/Asion001/mangarr/issues).

```
               ┌──────────────────────────── mangarr (Go) ─────────────────────────────┐
 Browser/API ─►│ API + embedded UI │ command queue/scheduler │ decision engine          │
 Komga apps  ─►│ Komga-compatible API (:25600) │ web reader │ SSE live updates          │
               │ pipeline: fetch pages → validate → [upscale/encode] → CBZ → import    │
               │ ─────────────────── module registry (Go interfaces) ───────────────── │
               │ source: native (own sites), suwayomi │ metadata: anilist, shikimori    │
               │ library: komga, kavita │ notify: telegram, discord, ntfy, gotify,      │
               │ apprise, webhook │ upscale: local, workers                             │
               │ worker task ledger ◄── /api/v1/worker/ ──────────────────────────────  │
               └────┬─────────────────┬───────────────┬────────────────────┬───────────┘
                    ▼                 ▼               ▼                    ▲
   manga sites (HTTP) ─► flaresolverr  metadata APIs  Komga/Kavita API    workers (pull)
   suwayomi (optional sidecar)                        (rescan + progress) download / upscale / encode
   mangarr writes /data/manga/<lang>/<Series>/*.cbz ─► Komga/Kavita (read-only mount) ─► apps
```

## Principles

- **The library works on its own.** Flat series folders, stable file names,
  `ComicInfo.xml`, `series.json` and `cover.jpg`. mangarr's database can be
  rebuilt from the files, and Komga or Kavita can read them without mangarr.
- **Everything external is a module** behind a Go interface. The core boots
  and works with none of them configured, and only
  `internal/modules/<kind>/<impl>` may talk to a vendor API
  (`scripts/import-lint.sh` enforces it).
- **Portable identities.** A source link is `(module instance, sourceId, url)`;
  for Keiyoushi-compatible sources `sourceId` is Mihon's id and the URL has no
  domain. Engine-specific ids (Suwayomi's integers) are a cache, so a library
  can move between engines (*Switch engine*) and survive a lost Suwayomi
  database.
- **SQLite and PostgreSQL are equal.** Every query goes through bun, migrations
  are kept per dialect, and CI runs every test on both.
- **Nothing lowers read progress** it didn't mean to. Progress from apps,
  library servers and the web reader is merged, never lowered by a server that
  doesn't know a chapter yet.

## Modules

Each module has a compiled implementation that registers itself, and
instances the user configures (kind, implementation, name, enabled, priority,
tags, settings JSON), in the style of Sonarr's ThingiProvider. The settings
struct's field tags become the form schema the UI renders; optional features
are capability interfaces discovered with type assertions. Details and the
interface table are in [modules.md](modules.md).

- **Sources**: `native` is mangarr's own sites, written against
  `internal/sources/sourcekit` (one file per site in `internal/sources/sites`);
  `suwayomi` drives a pinned Suwayomi for every other Keiyoushi extension.
  Catalog order, per-language and per-library priorities are resolved in
  `internal/sourcepriority`; request pacing per catalog in `internal/sourcegov`.
- **Metadata**: every enabled module is searched in parallel, results are
  joined by cross ids (AniList, MAL, MangaUpdates, …) or title, and merged
  field by field by priority (`internal/metadataagg`). Fields a user edits are
  locked against refreshes.

## Chapters and the pipeline

Chapter states: `missing` (wanted when monitored) → `queued` → `downloading` →
`processing` → `imported`, with `failed` beside them and `cleaned` for
chapters deleted after everyone read them (never wanted again unless
restored). Numbers come from the source; when a source has none, names are
parsed Mihon-style (`internal/chapternum`).

1. **Refresh** (`RefreshSources`, every 10 minutes) checks only the links that
   are due, with longer intervals for finished series and escalating backoff for
   failing sources.
2. **Decide** (`internal/decision`): monitored, not on disk or upgradable, not
   cleaned, blocklisted or queued, source healthy, scanlator allowed, enough
   pages, free space. Ranked by source priority, scanlator score, upload date.
3. **Download** into `/config/staging/<job>`, locally or on a worker. Every page
   is checked (magic bytes, decodes, not HTML, page count).
4. **Import**: a stored zip with `ComicInfo.xml` is written as
   `<name>.cbz.partial` in the series folder, fsynced and renamed to
   `<name>.cbz`, so readers never see half a file and no cross-mount move
   happens. History is recorded and a debounced library rescan is scheduled.
   After repeated failures the release is blocklisted and the next source is
   tried.
5. **Process** (upscale, re-encode) usually runs in the background after
   import. Upgrades and processing rename the new file over the **same path**,
   with the old one in the recycle bin, so Komga and Kavita keep read progress.

## Commands, tasks and events

Long work runs as commands in a persisted queue (`internal/jobs`): duplicates
return the existing command, exclusive and disk flags limit what runs
together, and commands still running at shutdown are requeued. The scheduler
ticks every 30 seconds; its tasks (refresh, metadata refresh, read-progress
sync, processing backlog, health checks, disk scan, backup, housekeeping,
extension updates) are listed with their intervals under System → Tasks.

A typed event bus feeds the SSE stream (`/api/v1/events`) the UI uses for live
updates, and the notification digests.

## Workers

A worker is the same binary with `MANGARR_MODE=worker` (or
`cmd/mangarr-worker` for builds without the server). It holds a key of its own
and polls the server for tasks from the ledger in `internal/worktasks`, so it
needs no inbound access. The server keeps everything that needs the module's
session or the site's budget: it resolves page addresses and hands each task
its share of the catalog's rate limit. Tasks have leases; a task nobody can
take comes back to the server, and a worker that dies loses its task to
another. Workers have priorities: a lower-priority worker gets a kind of task
only when every better-placed worker that can take it is full.

## Stack

- **HTTP**: `chi` and `huma/v2`, which generates the OpenAPI 3.1 document
  (`/api/docs`, committed as `web/openapi.json`); the UI gets typed calls
  through `openapi-typescript` and `openapi-fetch`.
- **Database**: `uptrace/bun` on `modernc.org/sqlite` (no cgo) or Postgres,
  with embedded goose migrations in `internal/db/migrations/{sqlite,postgres}`.
- **Web**: React, Vite, TypeScript, TanStack Query, Tailwind; embedded in the
  binary with `//go:embed`. UI strings live in
  `web/src/lib/i18n/messages.json` (English, Russian, Ukrainian), checked by
  `npm run check:i18n`.
- **Images**: `docker/Dockerfile` builds the full image (Vulkan, ncnn
  upscalers, `avifenc`, `cjxl`) and the distroless slim one.

## Where things live

| Path | What |
|---|---|
| `cmd/mangarr`, `cmd/mangarr-worker` | the server (also `env`, `openapi`, `bench` subcommands) and the worker-only build |
| `internal/api` | the REST API; `internal/komgaapi` the Komga-compatible one |
| `internal/app` | wiring: builds the services and registers tasks and commands |
| `internal/modules` | the registry, field schemas and every module implementation |
| `internal/sources` | `sourcekit` (the site toolkit, a leaf package) and `sites` |
| `internal/downloads`, `internal/processing`, `internal/cbz`, `internal/comicinfo`, `internal/library` | the pipeline |
| `internal/worker`, `internal/worktasks` | the worker process and the ledger of work handed to workers |
| `internal/reading`, `internal/progress`, `internal/readsync`, `internal/cleanup` | the web reader, progress merging, library-server sync and cleanup |
| `internal/access`, `internal/auth`, `internal/sso`, `internal/requests` | accounts, groups, sign-in and requests |
| `internal/envcfg` | environment variables; generates [configuration.md](configuration.md) |
| `web/` | the UI; `web/e2e` holds the Playwright flows |
