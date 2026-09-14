# Homelab setup

This guide assumes Docker Compose on one host (e.g. an Intel N100 box) with
the library on a shared dataset, Traefik in front, and an existing
FlareSolverr.

## 1. Folders and permissions

Pick one library root per language, e.g. `/data/storage/manga/en`. mangarr
writes there (read-write); Komga/Kavita only read it (`:ro`). Run mangarr and
the reader server with the same UID/GID (`user: "1000:1000"`) and keep the
default file mode `0664` / dir mode `0775`.

mangarr writes each CBZ as `<name>.cbz.partial` inside the series folder and
renames it when complete, so readers never see half-written files and no
cross-mount moves are needed.

## 2. Services

Start from [docker/compose.example.yml](../docker/compose.example.yml):

- **mangarr** — `/config` volume + the library.
- **suwayomi** — pin the tested version (`v2.3.2243`). Keep its web UI off and
  don't publish its port: extensions run as code inside it, and it doesn't
  need the library. `JAVA_TOOL_OPTIONS=-Xmx512m` + `mem_limit: 1g` keeps it
  around 400–700 MB. Only enable `KCEF_ENABLED` (Chromium WebView) if a
  source you need requires it (+ a few hundred MB RAM).
- **flaresolverr** — reuse your existing one (or Byparr).
- **komga** (or kavita) — library mounted read-only.
- **mangarr-upscaler** (optional) — see below.

Behind Traefik, route only mangarr (and Komga) publicly; mangarr has its own
login and API key. With an auth proxy in front you may set
`MANGARR_AUTH_DISABLED=true`.

## 3. First run

1. Create the admin account in the UI.
2. Settings → Media management → add the root folder(s). Keep the default
   naming `{Series Title} Ch.{Chapter:0000}`; don't put scanlator, source or
   volume in file names.
3. Settings → Source modules → *Suwayomi*: URL `http://suwayomi:4567`,
   *Use FlareSolverr* on, URL `http://flaresolverr:8191`. "Manage Suwayomi
   settings" turns off Suwayomi's own updater and auto-download (mangarr
   schedules everything). Press **Test**.
4. Sources → Extensions → install what you need (e.g. MangaDex).
5. Settings → Metadata → *AniList*.
6. Settings → Library servers → *Komga*:
   - URL `http://komga:25600`, an **admin** API key (Komga → Account →
     API keys).
   - Path mapping if the paths differ, e.g. `/data/manga` → `/manga`.
   - Komga has no file watcher; mangarr triggers the scan (debounced 30 s).
   - In Komga's library settings, enable "Empty trash after scan" if you use
     cleanup.
7. Settings → Notifications → e.g. *Telegram* (bot token + chat id).

## 4. Reading apps

Point your apps at Komga:

| App | How |
|---|---|
| Mihon (Android) | Komga extension (Keiyoushi repo); Komga tracker syncs progress |
| Paperback (iOS/iPad) | built-in Komga source |
| Tachimanga (iOS) | Komga |
| Panels / Chunky | OPDS `http://komga:25600/opds/v1.2/catalog` (Panels also has a Komga integration) |
| KOReader | OPDS + Komga's KOReader sync |

## 5. Readers and cleanup (optional)

Cleanup deletes chapters **every reader** has finished.

1. Settings → Readers → add each person, then **Link account** with *their
   own* Komga API key (Komga only exposes progress to the user itself).
2. Cleanup → enable, keep **Dry run** on at first, check the preview.
3. Defaults: ongoing series only, keep the last read chapter (keeps Mihon's
   tracker anchored), 7-day grace period, readers who never opened a series
   don't block it, `keep` tag excludes a series, files go to the recycle bin.
4. Cleaned chapters are never downloaded again; use **Restore** on a chapter
   to get it back.

## 6. Upscaling (optional)

Small pages look soft on an iPad. Profiles can upscale pages narrower than a
threshold (default 1400 px) with waifu2x / Real-CUGAN / Real-ESRGAN.

1. Run `mangarr-upscaler`:
   - On the N100: pass `/dev/dri` (Intel iGPU via Mesa ANV).
   - Or on a desktop with a GPU: run the image there (NVIDIA needs the
     container toolkit) and expose port 8788 on your LAN. Set `UPSCALER_TOKEN`.
2. Benchmark your hardware and pick a model:
   ```bash
   scripts/upscale-bench.sh http://host:8788 <token> "/data/manga/en/Series/Series Ch.0001.cbz" 4
   ```
   Rough guidance: `realesr-animevideov3` is fastest (good for color
   webtoons), `waifu2x-cunet` cleans black & white manga well, `realcugan`
   is sharper and slower.
3. Settings → Upscalers → add *mangarr-upscaler* with URL and token.
4. Settings → Profiles → enable upscaling, choose model, min/max width,
   WebP quality. New chapters are upscaled before import (original pages are
   used if the worker fails). Existing chapters: series page → **Upscale
   existing**.

## 7. PostgreSQL (optional)

For large libraries set `MANGARR_DB=postgres://mangarr:…@db:5432/mangarr?sslmode=disable`
(e.g. your existing Postgres 16). Built-in backups then contain settings and
modules only — back up the database with `pg_dump`.

## 8. Backups & upgrades

Daily backups (SQLite snapshot + manifest) go to `/config/backups`
(System → Backups). Suwayomi's own data is disposable: mangarr keeps the
real identity of every series (`sourceId` + URL) and re-links if Suwayomi's
database is lost. When upgrading Suwayomi, mangarr shows a health warning if
the version differs from the tested one.
