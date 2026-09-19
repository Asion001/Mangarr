# Plan: mangarr, a Sonarr-style manga PVR

## Context
The user runs a Docker homelab: Intel N100 with 10 GiB RAM and an Intel iGPU passed through, plus
Sonarr, Prowlarr, qBittorrent, FlareSolverr, Traefik, Cloudflare Access and Telegram
notifications. They want a Sonarr equivalent for manga that:
- monitors series and detects new chapters
- downloads them from **Keiyoushi (Mihon/Tachiyomi) extension sources**, getting past Cloudflare
  with FlareSolverr
- sends notifications
- keeps a **complete local library that works on its own**

Reading happens in apps on iPad, Android and desktop.

Decisions made with the user:
- **Files only, no built-in reader.** mangarr writes CBZ files with ComicInfo.xml. **Komga**
  (recommended) or Kavita serves them to Mihon, Paperback/Tachimanga, Panels, Chunky and KOReader.
  mangarr triggers a rescan after each change.
- **Go core.** One static binary with the React UI built in. **SQLite by default, PostgreSQL as an
  option** for larger libraries. The repo is
  `~/dev/my/mangarr`: currently empty, remote `git@github.com:Asion001/mangarr.git`.
- **Everything external is a replaceable module** behind a Go interface: sources, metadata, library
  servers, notifications and upscalers. **Suwayomi is only the first source module.** The core
  boots and works without it, and another implementation can replace it.
- **Metadata comes from several modules**, searched by priority, with the results merged. v1 ships
  the framework plus AniList, with the linked source's own details as a fallback. More modules
  come later.
- **v1 also includes:**
  - **Read-based cleanup** (off by default): delete chapters every reader has finished, keeping the
    last N and waiting a grace period.
  - **Page upscaling**, because small pages look bad on the iPad.
- **v1 sources:** Keiyoushi only. Torrent, DDL and importing existing files come later.

## Architecture
```
               ┌──────────────────────────── mangarr (Go) ─────────────────────────────┐
 Browser/API ─►│ API + embedded UI │ command queue/scheduler │ decision engine          │
               │ pipeline: fetch pages → validate → [upscale] → CBZ → atomic import    │
               │ ─────────────────── module registry (Go interfaces) ───────────────── │
               │ source: native (own sites), suwayomi │ metadata: anilist, source       │
               │ library: komga, kavita │ notify: telegram, discord, ntfy, gotify,      │
               │ apprise, webhook │ upscale: local, workers                             │
               │ worker task ledger ◄── /api/v1/worker/ ──────────────────────────────  │
               └────┬─────────────────┬───────────────┬────────────────────┬───────────┘
                    ▼                 ▼               ▼                    ▲
   manga sites (HTTP) ─► flaresolverr  anilist.co   Komga/Kavita API    workers (pull)
   suwayomi (optional sidecar)         (rescan + per-reader progress)   download / upscale / encode
   mangarr writes /data/manga/<lang>/<Series>/*.cbz ─► Komga/Kavita (read-only mount) ─► apps
```

## Module system (the core pattern)
Every module has two parts:
- **A compiled implementation** that registers itself in a registry.
- **Instances configured by the user**, stored as `ProviderDefinition` rows: kind, implementation,
  name, enabled, priority, tags, settings JSON. This is Sonarr's ThingiProvider pattern.

Each implementation declares:
- a settings struct whose field tags (label, type, help, order, advanced, secret) are served as
  field definitions at `GET /api/v1/{kind}/schema`, so the UI can render its form generically
- `Test(ctx)`
- optional capability interfaces, discovered with Go type assertions

`internal/modules/<kind>/<impl>` packages are the only ones allowed to import vendor clients.
An import-lint rule in CI enforces this.

| Kind | Interface (short form) | Optional capabilities | v1 implementations |
|---|---|---|---|
| `source` | `Sources()`, `Search(src, q, page, filters)`, `Series(ref) (details, chapters)`, `Pages(chRef)`, `FetchPage(pageRef) io.ReadCloser` | `ExtensionManager` (stores/install/update), `Latest`, `Filters`, `SourcePreferences`, `Fetchable` (a request another machine can make) | `native`, `suwayomi` |
| `metadata` | `Search(q) []Candidate`, `Get(id) SeriesMetadata` | `LookupByExternalID(kind, id)` (cross-provider join) | `anilist`, `source` (fallback from the linked source's details) |
| `library` | `Rescan(paths)`, `Test` | `ReadProgress(account, seriesPath) []BookProgress` | `komga`, `kavita` |
| `notify` | `Send(event)` | batching hints | telegram, discord, ntfy, gotify, apprise, webhook |
| `upscale` | `Info() (models, devices)`, `Upscale(ctx, pages, params) pages` | — | `local`, `workers` |

**Portable source identity.**
- A series source link stores `(moduleInstanceId, sourceId, mangaUrl)` and a chapter stores
  `chapterUrl`.
- For Keiyoushi, `sourceId` is Mihon's deterministic MD5-based ID and URLs don't include the domain.
  So any Tachiyomi-compatible module can take over a link by swapping `moduleInstanceId`, and a
  "reassign source module" command does exactly that.
- Engine-specific IDs, such as Suwayomi's integer IDs, are cached only.

### Source module v1: `suwayomi` (headless sidecar)
Everything below was verified against Suwayomi's source.

- **Container:** image pinned to e.g. `ghcr.io/suwayomi/suwayomi-server:v2.3.2243`.
  - Settings: `WEB_UI_ENABLED=false`, `KCEF_ENABLED=false` (turn it on only for sources that need
    WebView), `-Xmx512m`, `mem_limit: 1g`.
  - No published ports and **no library mount**. Extension code runs with full JVM rights, so this
    keeps it away from the user's files.
- **Settings reconciler.** On connect, the module calls `setSettings` and keeps checking for drift.
  It enforces:
  - `globalUpdateInterval=0` and `autoDownloadNewChapters=false`
  - the FlareSolverr fields, edited in the module's settings form
  - `extensionStores` set to the Keiyoushi repo
- **Typed client:** `genqlient` generates it from `.graphql` operation files and a pinned
  `schema.graphql`.
  - At startup the module compares `aboutServer` and checks every operation still exists. A
    mismatch shows as a health issue.
  - Every operation is request/response, so no WebSocket subscriptions are needed.
- **Operations:**
  - Extensions: `fetchExtensions`, `updateExtension(s)`
  - Sources: `sources`, `source(id){filters preferences}`, `updateSourcePreference`
  - Browse and search: `fetchSourceManga(type: SEARCH|POPULAR|LATEST)`
  - Details and chapters: `fetchMangaAndChapters`
  - Pages: `fetchChapterPages`, which returns `/api/v1/manga/{id}/chapter/{sourceOrder}/page/{i}`.
    It is called right before a download, because `sourceOrder` shifts when a source adds chapters.
- **Re-linking IDs:** when a cached ID misses, re-resolve with `mangas(condition:{sourceId,url})`.
  If that fails, search the source and match on the URL.
- **Image cache:** clear Suwayomi's cache periodically.

### Metadata aggregation
- **Search:** query every enabled metadata module in parallel. Merge and de-duplicate the results
  by cross IDs (AniList↔MAL↔MU↔MangaDex) or by normalized title plus year, and rank them by module
  priority.
- **Enrich:** on add and on refresh, use the primary module's data and fill gaps from lower-priority
  modules (joined by cross ID, or by title match above a threshold).
- **Merge rules:**
  - Scalar fields: the first non-empty value by priority.
  - Lists (alternate titles, genres, tags, links, authors): union of all modules, de-duplicated.
  - Cover: the highest-priority one above a minimum resolution.
- **Provenance and locks:** each field records which module supplied it. Fields the user edits get
  a lock, as in Komga, so refreshes don't overwrite them.
- A series can also be **source-only**, with metadata coming from its source module.

## Domain model
| Entity | Key fields |
|---|---|
| Series | title, sortTitle, altTitles, externalIds (JSON), status, monitored, monitorNew, rootFolderId, path, profileId, readingDirection, language, tags, metadata provenance/locks |
| SeriesSource | seriesId, moduleInstanceId, sourceId, sourceName, lang, mangaUrl, engineRef (cache), priority, enabled, checkInterval, lastChecked/lastSuccess, consecutiveFailures, backoffUntil |
| Chapter | seriesId, numberKey (canonical string), numberSort (float64), volume?, title, monitored, state, fileId, cleanedAt |
| ChapterRelease | chapterId, seriesSourceId, chapterUrl, engineRef, name, scanlator, uploadDate |
| ChapterFile | chapterId, relativePath, size, pageCount, avgWidth, format, releaseId, sha256, upscaled (model, scale, sizeBefore), importedAt |
| Reader / ReaderAccount | person ↔ one account per library module instance (e.g. that user's Komga API key) |
| ChapterReadState | readerId, chapterId, completed, page, readAt (synced from library modules) |
| Profile | scanlator preferences/blocks, allowUpgrades + cutoff, minPages, upscale settings, cleanup overrides |
| History, Blocklist, DownloadJob, Command, RootFolder, Tag, ProviderDefinition | |

- **Chapter states:** Missing (wanted if monitored) → Queued → Downloading → Processing (upscale) →
  Imported → **Cleaned** (deleted after being read; never wanted again unless the user restores it).
  Failed and Blocked are side states.
- **Chapter numbers:** use the extension's float32 `chapterNumber`, rounded to 3 decimals. If it is
  -1, parse the name with a port of Mihon's ChapterRecognition regex. Anything still without a
  number goes to manual mapping.

## Pipelines
1. **Add series.**
   1. Search metadata across modules, or skip for a source-only series.
   2. Search the enabled sources for the title and its synonyms.
   3. Link one or more sources and set their priority.
   4. Sync chapters.
   5. Choose a monitor option: All / Future / From chapter N / Latest N / None. By default, older
      chapters are not mass-downloaded.
2. **Refresh** (the equivalent of Sonarr's RSS sync): for each SeriesSource that is due, call
   `Series()` and diff the result into ChapterRelease rows.
   - Intervals: 6 h by default, 24 h when on hiatus, 7 d when completed, with jitter.
   - Concurrency: 1–2 per source, plus a global limit.
   - Failures back off on an escalating schedule (1 min … 24 h).
3. **Decision engine** (specs run cheapest first):
   - Specs: Monitored, NotOnDisk-or-Upgradable, NotCleaned, NotBlocklisted, NotQueued,
     SourceHealthy, ScanlatorAllowed, MinPages, FreeSpace.
   - Ranking: source priority, then scanlator score, then earliest upload.
4. **Download → process → import.**
   1. Fetch pages into `/config/staging/<job>`: 3 workers globally, 1 per source, retry each page
      with backoff.
   2. Validate each page: magic bytes (JPEG/PNG/WebP/GIF/AVIF), `image.DecodeConfig`, not HTML, and
      the page count.
   3. **Upscale** if enabled (see below).
   4. Stream a stored (uncompressed) zip with pages `0001.ext` and `ComicInfo.xml` into the series
      folder as `<name>.cbz.partial`. Neither Komga nor Kavita scans that extension.
   5. fsync, then rename in the same folder to `<name>.cbz`. The rename is atomic and avoids
      cross-mount errors.
   6. Record history, emit events, and schedule a debounced rescan.
   7. After N failures, blocklist the release and try the next source.
5. **Upgrade or re-process.** Copy or hardlink the old file into the recycle bin, then rename the new
   file over the **same path**. Komga keeps the book ID and Kavita keeps the chapter, so read
   progress survives.
6. **Disk scan.** Check files against the database. Deleted files make their chapters Missing again,
   except Cleaned chapters. The database can be rebuilt from the files.

## Read tracking and cleanup (v1, off by default)
**Readers:** under Settings → Readers, each person gets a Reader with one account per library
module.
- In Komga, read progress and API keys belong to each user: the admin can't read another user's
  progress. So each person creates an API key in Komga (`me/api-keys`) and pastes it in.
- Kavita works the same way: each user's API key is exchanged for a login token via
  `POST /api/Plugin/authenticate`.

**Sync task** (every 30 min, configurable). For each reader account:
- **Komga:** `GET /api/v1/series/{id}/books?unpaged=true` as that user, reading
  `readProgress{completed, readDate, page}`.
- **Kavita:** series detail, comparing `pagesRead` with `pages`.
- Books are matched to our ChapterFiles by file path, through the library path mapping. Results are
  stored in ChapterReadState. The UI shows per-series progress for each reader.

**Cleanup settings** (global, overridable per profile, series or tag):

| Setting | Default | Meaning |
|---|---|---|
| `enabled` | off | |
| `dryRun` | on for the first enable | Shows a preview list with sizes |
| `statuses` | ongoing only | Or all statuses |
| `readers` | all | Or a selected subset |
| `ignoreReadersNotStarted` | on | A reader who never opened the series doesn't block its cleanup |
| `keepLastRead` | 1 | Keeps Komga's series progress anchored for Mihon's tracker |
| `graceDays` | 7 | Days after the last required reader finished the chapter |
| `minFreeSpaceGB` | 0 | 0 = always; otherwise run only when free space is below this |
| `excludeTags` | `keep` | |
| recycle-bin retention | 7 d | |

**Algorithm, for each series in scope:**
1. R = the required readers, minus those who haven't started the series (when that setting is on).
   Skip the series if R is empty.
2. Candidates are files with every reader in R having `completed=true`, minus the last
   `keepLastRead` completed chapters, and only where `now − max(readAt) ≥ graceDays`.
3. Move each candidate to the recycle bin, set Chapter `state=Cleaned` with `cleanedAt`, write
   history, then send a debounced rescan and a "freed X GB" digest notification.
4. A "Restore" action re-downloads a Cleaned chapter.
5. Tell the user in the docs to enable "empty trash after scan" in Komga.

## Upscaling (v1, off by default, configured per profile or series)
**Upscale modules:** `local` runs the engine in the server process; `workers` hands batches to the
machines that have the upscale role (see *Workers*). Both wrap the same engine.
- It wraps the ncnn/Vulkan CLIs `waifu2x-ncnn-vulkan` and `realcugan-ncnn-vulkan` (both MIT) and
  `realesrgan-ncnn-vulkan`, running each on a whole folder at a time so every chapter loads the
  model once.
- One job at a time per GPU, whichever module drives it.
- Docker image: `debian:trixie-slim` with `mesa-vulkan-drivers` (Intel ANV, plus lavapipe as a CPU
  fallback for CI), the binaries and the models.
  - It can run on the N100 with `/dev/dri` passed through.
  - A Windows build (`cmd/mangarr-worker` plus the ncnn binaries) can run on the desktop GPU.
  - Any number of workers; each has its own key and roles.
- The core image stays distroless.

**Settings:**

| Setting | Default | Meaning |
|---|---|---|
| `enabled` | off | |
| `minWidth` | 1400 px | Only narrower pages are upscaled; this covers normal pages and webtoon strips |
| `scale` | 2× | Auto 2× or 4× to reach `minWidth`, then Catmull-Rom downscale to `maxWidth` 2048 |
| `model` + `noise` | picked by the benchmark spike | Candidates: waifu2x-cunet for B/W, realesr-animevideov3 for color |
| `format` | WebP q90 | Or JPEG, PNG, or keep; WebP limits the size growth |
| `timing` | before import | On worker failure, import the original, mark it not upscaled, and raise a health warning |

**"Upscale existing chapters"** is a per-series or bulk command that re-processes files in place,
using the same-path rename.

**Later:** a MangaJaNai/IllustrationJaNai module (PyTorch or ONNX, NVIDIA desktop) for the best
quality on B/W and color pages.

## Library layout and ComicInfo (works with Komga and Kavita)
```
/data/manga/en/                        ← one root folder per language
  One Piece/
    cover.jpg  series.json             ← series.json: Mylar schema with ALL required fields
    One Piece Ch.0001.cbz
    One Piece Ch.1044.5.cbz
```
- **Naming template:** `{Series Title} Ch.{Chapter:0000}` by default. Folders stay flat. No
  scanlator, source or volume in the name, since changing them later breaks read progress; volume
  is an opt-in token.
- **ComicInfo.xml in every CBZ:**
  - Numbering: `Series`, `Number` (always written).
  - Text: `Title`, `Summary` (the series synopsis).
  - People and tags: `Writer`, `Penciller`, `Translator`=scanlator, `Genre`, `Tags`.
  - Date: `Year`/`Month`/`Day`.
  - `Web`: the source URL plus the AniList URL.
  - `LanguageISO`, `Manga=YesAndRightToLeft` or `No`, `AgeRating`, `Count` when the series has
    ended, `PageCount`.
- **Mounts:** mangarr gets `/data/storage/manga:/data/manga` read-write, with a shared GID and
  umask 002. Komga and Kavita get the same folder read-only. Library modules have a path-mapping
  setting.

## Automation, notifications and health
- **Command queue and scheduler**, persisted in SQLite, with Sonarr's rules:
  - A duplicate command returns the existing one.
  - Exclusive, long-running and disk flags limit what can run together.
  - Commands still running at shutdown are requeued at startup.
  - A tick every 30 s.
- **Tasks:**

  | Task | Schedule |
  |---|---|
  | RefreshSources | every 10 min, picks only series that are due |
  | ProcessQueue | continuous |
  | ProcessUpscale | separate pool, one job per worker |
  | SyncReadProgress | 30 min |
  | Cleanup | runs after each read-progress sync |
  | RefreshMetadata | 24 h |
  | ExtensionUpdateCheck | 12 h, for modules with the ExtensionManager capability |
  | HealthCheck | 5 min, plus on events |
  | DiskScan | 24 h |
  | Backup | daily: SQLite `VACUUM INTO` + config |
  | Housekeeping and recycle-bin purge | daily |

- **Notification events:**
  - ChapterImported, **batched into a per-series digest**
  - Upgraded, SeriesAdded/Deleted, DownloadFailed (all sources tried)
  - CleanupDone (digest of space freed)
  - HealthIssue/Restored, ExtensionUpdateAvailable, ManualInteractionRequired
  - Tags filter which series notify which instance.
- **Health checks:**
  - Each module instance's `Test` result; Suwayomi's schema matches and its settings haven't
    drifted.
  - Root folders are writable and have free space.
  - Sources that are failing or backed off.
  - Reader account authentication.
  - The upscaler is reachable and has a GPU.
  - Sources whose link is lost.
- **Auth:** forms login plus an `X-Api-Key` header. Live UI updates go over an SSE stream of
  resource changes.

## Web UI (React, Sonarr-like)
- **Series:** grid or table.
- **Add series:** multi-module metadata search, then source matching, then monitor options.
- **Series detail:**
  - chapters and releases, with per-reader read markers
  - sources, with priority, health, re-link and module reassignment
  - monitoring, the profile, and metadata with provenance and locks
- **Activity:** queue with page and upscale progress, history, blocklist.
- **Wanted:** missing chapters.
- **Cleanup:** preview, run now, freed-space history.
- **Settings:**
  - Modules: Sources (with an extension manager when the module supports it), Metadata, Library
    servers, Notifications, Upscalers
  - Readers, Profiles, Media management (root folders, naming preview), Cleanup, General
- **System:** health, tasks, logs, backups.

## Tech stack (Go 1.27 is installed locally)
- **HTTP:** `chi` plus `huma/v2`, which generates an OpenAPI 3.1 spec. The UI gets typed calls
  through `openapi-typescript` and `openapi-fetch`.
- **Database: SQLite by default, PostgreSQL as an option** for large libraries.
  - Selected with one DSN: `MANGARR_DB=sqlite:///config/mangarr.db` (default) or
    `postgres://…@db_16:5432/mangarr`, which could reuse the existing PG16.
  - **`uptrace/bun`** is the query builder and ORM, with `sqlitedialect` on `modernc.org/sqlite`
    (no cgo) and `pgdialect` on `pgx`. All app queries go through bun, and raw SQL is only allowed
    through a small per-dialect helper.
  - **goose** runs embedded migrations, kept as separate SQL folders `db/migrations/{sqlite,postgres}`
    because the DDL differs (types, identity columns, JSONB vs TEXT).
  - SQLite: WAL mode, a busy timeout, a single writer connection plus a read pool.
  - Postgres: a pgx pool; JSON columns become JSONB with GIN indexes where filters need them.
  - Backups: SQLite uses `VACUUM INTO`. For Postgres the built-in backup covers config only, and the
    docs point to `pg_dump`.
- **GraphQL clients:** `genqlient` for Suwayomi and AniList.
- **Files and images:** `archive/zip` (stored mode); `image` with `x/image/webp` and `x/image/draw`
  for validation and resizing.
- **Runtime:** `log/slog` with a ring buffer for the UI; a typed event bus feeding the SSE hub.
- **Web:** React, Vite, TypeScript, TanStack Query and Router, Tailwind and shadcn/ui, embedded with
  `//go:embed`. In development, Vite proxies to `air`.
- **Build:** a multi-stage Dockerfile ending in distroless/static. The images
  `ghcr.io/asion001/mangarr` (full and slim) is multi-arch, built by GitHub Actions: lint,
  import-lint, test, build. The same image is the server and the worker (`MANGARR_MODE`).

**Repo layout:**
```
cmd/mangarr/  cmd/mangarr-worker/
internal/
  api/  events/  jobs/  health/  config/
  db/            open(dsn) → bun.DB (sqlite|postgres), migrations/{sqlite,postgres}, repositories
  domain/        series, chapters, releases, files, readers, chapter-number parser
  decision/      specs + ranking
  pipeline/      pagefetch, validate, process(upscale), cbz, comicinfo, importer, staging
  library/       naming, diskscan, seriesjson, cover, recyclebin
  cleanup/       rules engine, preview, executor
  metadata/      aggregator, merge, provenance
  modules/
    registry.go  provider.go  fields.go      (definitions, schema, Test)
    source/{source.go, native/, suwayomi/}
    metadata/{metadata.go, anilist/, sourcemeta/}
    library/{library.go, komga/, kavita/}
    notify/{notify.go, telegram/, discord/, ntfy/, gotify/, apprise/, webhook/}
    upscale/{upscale.go, local/, workers/}
  sources/       sourcekit/ (the site SDK: leaf package) + sites/ (one file per site)
  upscaler/      the ncnn engine (in the server, or on a worker)
  worker/        the worker process: lease → do → upload
  worktasks/     the ledger of work handed to workers
web/
docker/{Dockerfile, compose.example.yml}   (mangarr, optional suwayomi/flaresolverr, komga, workers)
```

## Milestones
- **M0 – Bootstrap:**
  - Repo, CI, Dockerfiles, compose file.
  - Config, auth, API skeleton, SSE, UI shell.
  - The database layer supports **both SQLite and Postgres from day one**, with a CI matrix, so
    SQLite-only code can't creep in.
  - **Module registry with field schemas, Test, and ProviderDefinition CRUD.**
- **S1 – Upscaler spike** (in parallel, early): benchmark waifu2x, realcugan and realesrgan (ncnn) on
  the N100 iGPU in the Incus container, and on the desktop GPU. Measure seconds per page and quality
  at 2×, then pick the default models.
- **M1 – Source module `suwayomi`:** codegen, settings reconciler, extension manager, sources,
  search. Plus the generic Sources UI.
- **M2 – Metadata and series:**
  - AniList and source-fallback modules, the aggregator with merge, provenance and locks.
  - Add-series flow, chapter sync and numbering, monitor options.
- **M3 – Download and import:**
  - Queue, page fetch and validation.
  - CBZ, ComicInfo, series.json and cover; naming; atomic import.
  - History, blocklist, falling back to the next source.
- **M4 – Automation:** scheduler, refresh with backoff, decision engine, Wanted, upgrades, disk scan.
- **M5 – Library and notification modules:**
  - Komga and Kavita rescans with path mapping.
  - Six notification modules with digests.
  - Health checks, the System pages, backups.
- **M6 – Read tracking and cleanup:** readers and accounts, progress sync, the rules engine, preview
  and dry-run, recycle bin, the Cleaned state and restore.
- **M7 – Upscaling:**
  - The ncnn engine and its image, in the server and on workers.
  - The pipeline stage with fallback, "upscale existing", settings UI.
- **Later:**
  - Metadata modules: MangaUpdates, MangaDex, MAL/Jikan, Kitsu, ComicVine.
  - Source modules: native MangaDex, a custom JVM host, Prowlarr/qBittorrent, DDL.
  - Upscaler module: MangaJaNai.
  - Import existing files, `.tachibk` re-linking, delay profiles, smarter intervals, import lists.
  - `mangarr db copy --to postgres` for moving an existing SQLite install.

## Verification
- **Unit tests** (`go test ./...`, table tests and golden files):
  - chapter-number parser, with fixtures taken from real Keiyoushi chapter names
  - naming templates; ComicInfo output, checked in CI with `xmllint --schema ComicInfo.xsd`
  - series.json required fields
  - decision specs and ranking; metadata merge and locks
  - **cleanup rules**: several readers, not-started readers, keepLastRead, grace days, tags, dry-run
  - CBZ writer: partial file then rename
- **Repository and migration tests** run twice in CI: once on SQLite and once on Postgres 16
  (testcontainers).
- **Module contract tests:** one shared test suite per kind runs against each implementation, using
  `httptest` fakes for Komga, Kavita, AniList and the notification targets. A fake source module
  drives pipeline tests without Suwayomi, which proves the core doesn't depend on it.
- **Integration tests** (`-tags integration`, testcontainers-go):
  - The pinned Suwayomi adds the Keiyoushi store, installs MangaDex, and fetches one chapter.
  - Komga is created with two users and their API keys; read progress is marked for each user, then
    the sync runs and cleanup deletes the expected files.
- **End-to-end on the homelab**, with `docker compose up`:
  1. Add the Keiyoushi store and install MangaDex plus one Cloudflare-protected source.
  2. Add a series with Latest 3. Check that the CBZ files appear, check `unzip -p … ComicInfo.xml`,
     check that Komga's scan ran, and read in Mihon (Komga extension) and Panels.
  3. Turn on upscaling. Check the page widths in a CBZ and compare them on the iPad.
  4. Add two readers, read chapters in Mihon as both, run a cleanup preview, then run it live. The
     files should go to the recycle bin, the chapters should become Cleaned and should not be
     downloaded again, and a digest should arrive.
  5. Stop Suwayomi. The UI should keep working and a health issue should fire.
  6. `docker stats`: mangarr should stay under about 80 MB and Suwayomi under 1 GB.

## Risks and mitigations
- **Suwayomi's GraphQL changes without versioning:** pin the version, generate the client, check the
  schema at startup, keep it behind the module boundary, and keep IDs portable.
- **Upscaling is too slow on the N100 iGPU:** the S1 spike measures it first. The worker can run on
  the desktop GPU instead, only narrow pages are processed, and processing runs one job at a time.
- **Upscaling makes files bigger:** WebP q90, a `maxWidth` cap, and cleanup to reclaim space.
- **Cleanup deletes something wanted:** off by default, dry-run on the first enable, a preview, a
  `keep` tag, `keepLastRead ≥ 1`, and a recycle bin with a restore action.
- **The SQLite and Postgres behavior could drift apart:** bun is the only query path, migrations are
  kept per dialect, and CI runs every test on both.
- **Per-user API keys** are needed for progress. This is documented, and account authentication has
  a health check.
- **Suwayomi's H2 database can corrupt:** mangarr is the source of truth and the IDs are portable,
  so links can be recovered. Optionally point Suwayomi at the existing Postgres 16.


## Phase 2 implementation sequence

Each part is committed separately on `codex/phase-2`. Existing permission checks remain authoritative.

| Part | Implementation and verification |
| --- | --- |
| P1 | Account UI preferences (`auto/en/ru/uk`, `ua` alias), SQLite/Postgres migrations and portable backups; Russian/Ukrainian catalog, locale-aware dates, unit and browser checks. Static UI copy translated; dynamic templates and built-in module copy audited again in P10. |
| P2 | Reading/editing switch; collapsible desktop rail, scrolling navigation, fixed account controls and accessible mobile drawer. |
| P3 | Signed size delta, space added/net saved statistics, useful slow-job throughput and responsive history. |
| P4 | Reproduce Mihon tracker requests, repair stale Komga sessions and verify numbering/progress contracts. Record actual Mihon validation separately. |
| P5 | Language/library source preferences, per-series inheritance/custom mode, shared resolver for candidate/current-file comparisons and reader fallback. |
| P6 | Scoped, paginated title search and chapter navigation, stable sorting, filters and shared chapter picker. |
| P7 | Cached general discovery feeds from metadata and prioritized sources; scoped enrichment, source-backed requests and partial failures. |
| P8 | Responsive discovery shelves, filters and detail actions using existing permissions and request flow. |
| P9 | Caller-scoped `.tachibk` library/progress and Komga settings export, revocable device key and restore guide. |
| P10 | Cross-database/migration/backup checks, browser coverage, generated schema checks, CI and live integration evidence. |

P1 validation: API, schema coverage and database-copy tests passed against SQLite and local PostgreSQL; locale unit tests passed.
Browser validation: Chromium switched Russian/Ukrainian and retained the anonymous preference after reload. Production build and 1,097-message static coverage check passed.

P2 implemented: reading is the default even for administrators, with explicit editing controls and permission-aware direct links. Desktop navigation collapses to a saved icon rail; mobile navigation scrolls independently of the account footer and supports focus trapping, Escape and focus restoration. Chromium tests cover both modes, denied access, persistence and a 390×600 expanded menu; frontend typecheck/build and locale checks passed.

P3 implemented: positive growth and negative reduction use one signed percentage; unknown originals show no percentage. Gross saved is preserved for existing API consumers, with additive spaceAdded and signed netSpaceSaved. Slow throughput uses pages/minute, the chart includes negative savings, and mobile history uses labeled cards. API regressions cover mixed savings/growth/unknown sizes; unit and Chromium checks cover the reported 13 pages / 17m26s case. Typecheck, production build and locale checks passed.

P4 implemented: valid API-key requests now replace invalid, expired, near-expiry or different-device Komga cookies, allowing Mihon's credential-free tracker requests to use the refreshed session. Cookie-only GET/PUT regression flows and existing Komga tests passed on SQLite and PostgreSQL, including v1 index/v2 fractional-number contracts. See MIHON.md for upstream protocol references and the outstanding real-device check; the user's exact Mihon error is not claimed resolved end to end.

P5 implemented: source priorities can inherit from the global order or override it per root folder and language. Candidate selection, existing-file comparison and reader fallback use the same resolver. SQLite/PostgreSQL migrations, database-copy coverage and resolver/API tests passed.

P6 implemented: library search, filters, stable sorting and pagination run on the server while preserving grouped language editions and aggregate counts. The reader chapter picker supports text search, read/availability filters and ascending or descending chapter order. API, unit and Chromium tests passed.

P7 implemented: `/api/v1/discover` combines personalized unread-library recommendations, recent library changes and popular titles from prioritized source catalogs. Popular results use the existing 30-minute source cache, bounded concurrency and partial-failure reporting; signed thumbnails remain usable by ordinary signed-in readers. SQLite/PostgreSQL API tests passed.

P8 implemented: Discover is available in reading and editing modes with a recommendation spotlight, continue-reading row, horizontally scrollable recommendation/update/source shelves, and permission-aware open/add/request actions. Relevant live events invalidate its cache. Russian/Ukrainian coverage, typecheck, unit tests, production build and nine Chromium flows passed.
