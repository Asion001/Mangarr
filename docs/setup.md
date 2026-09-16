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

Let the proxy terminate TLS: browsers only use HTTP/2 over HTTPS, and it's
worth having — over plain HTTP/1.1 a browser opens about six connections per
site and the live-updates stream holds one of them, which the reader feels
when it loads pages. mangarr itself speaks HTTP/1.1 and cleartext HTTP/2
(h2c), so Traefik, Caddy and nginx can forward either.

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

There are two ways to read.

**Straight from mangarr (recommended).** mangarr has a Komga-compatible API.
Komga apps connect to mangarr as if it were a Komga server, and see **every
series and every chapter mangarr knows**, downloaded or not. Chapters that
aren't downloaded are streamed from the source; opening one also queues its
download. Progress syncs both ways.

1. Settings → **Reading apps** → *Allow Komga apps to connect*. The API
   listens on its own port, `25600` like Komga (`MANGARR_KOMGA_LISTEN`).
   Publish it, e.g. `"25601:25600"` when Komga already uses 25600 on the
   host, and set *Address apps should use* to the address the apps reach.
2. **Add device** for each app. Each one gets its own API key, so you can see
   what every device synced and revoke one without the others. Apps that only
   ask for a username and password (Paperback) take any username with the
   key as the password.
3. Connect the apps (the page has step-by-step guides with your address):

| App | How | Progress |
|---|---|---|
| Mihon (Android) | Komga extension (Keiyoushi repo): address + API key | Enable **Komga** under Settings → Tracking → enhanced services. Syncs finished chapters. |
| KMReader (iPhone, iPad) | Add server: address + API key, or username and password | Page by page, live updates. Downloaded chapters can be saved offline. |
| Paperback (iPhone, iPad) | Komga extension: address, any username and a device key as the password (or your mangarr login) | Finished chapters, through its Komga tracker |

Each device key belongs to the account that made it, so everyone's progress
stays their own; keys made before accounts existed (and keys of the API key)
use the reader under Settings → Reading apps. What every device reports is
logged under Settings → Readers → *Devices & sync*, next to what Komga and
Kavita report. mangarr passes every change on to
the library servers, so Komga's own web reader stays up to date too. It
never lowers progress, except when you mark a chapter unread in an app.

*Read ahead* (on by default) downloads the next 3 chapters after the
furthest one a reader has started, in any app or library server, even in
series that aren't monitored. The Series page shows a **Continue reading**
row with the next chapter of each series in progress. The apps get the same
list as *On deck* and as a *Continue reading* read list.

**Through Komga or Kavita.** Apps can also read the library through Komga
or Kavita. They then only see downloaded chapters:

| App | How |
|---|---|
| Mihon (Android) | Komga extension (Keiyoushi repo); Komga tracker syncs progress |
| Paperback (iOS/iPad) | built-in Komga source |
| Tachimanga (iOS) | Komga |
| Panels / Chunky | OPDS `http://komga:25600/opds/v1.2/catalog` (Panels also has a Komga integration) |
| KOReader | OPDS + Komga's KOReader sync |

## 5. Accounts: reading together

Everyone who reads here gets an account, with their own progress, their own
devices and their own notifications. The library itself is shared.

1. **Settings → Users & groups → Invites → Create an invite**: pick the
   group, how many people may use it and when it expires. Send the link; they
   choose a username and password (or single sign-on, see §8) and are in. The
   invite is spent when it runs out of uses.
2. **Groups** carry the permissions and what part of the library their
   members see:

   | Permission | What it allows |
   |---|---|
   | Administrator | settings, modules, system, users |
   | Manage the library | add, edit and delete series, the queue, sources, history |
   | Handle requests | see, add and decline what people ask for |
   | Request series | ask for series to be added |
   | Reading apps | the Komga-compatible API and device keys |
   | Download files | the CBZ of a chapter |

   Reading the library they can see, their own progress and their own account
   need no permission. Built-in **Admins** and **Users** can't be deleted.
3. **What they see**: a group can be limited to series with certain tags,
   without others, or in certain root folders. Series outside it don't exist
   for its members — not in the library, the web reader, the Komga API or the
   search.
4. **Their own progress**: each account gets a reader of its own (Settings →
   Readers). Progress from their apps, the web reader and their own
   Komga/Kavita account (§9) all land there. On a series page an
   administrator sees how far everyone got; others see only themselves.
5. **Sessions**: My account lists where you're signed in, and signs other
   sessions out. Changing a password, disabling an account or *Sign out
   everywhere* ends its sessions at once.
6. **Login protection**: 10 failed attempts for the same name or address
   (30 from one address) lock signing in there for 15 minutes; it covers the
   web login, invite links and the Komga API.

## 6. Requests, following series and personal notifications

**Requests** (Jellyseerr-style) let people ask for series without giving them
the run of the library.

- Someone with *Request series* opens **Requests → Request a series**,
  searches the metadata providers and asks, with a note if they like. Asking
  for something someone already asked for joins their request.
- People with *Handle requests* (or *Manage the library*) see them under
  **Requests → To handle**: **Add series** opens the usual add flow with the
  metadata filled in and links the request, **Link** points it at a series
  that's already there under another name, **Decline** gives a reason.
- A request becomes *Approved* when the series is in the library and
  *Available* when its first chapter is imported. Whoever asked follows the
  series and hears about each step.
- A group can have **Add members' requests without approval** turned on: when
  Quick search finds a confident source, the series is added straight away;
  otherwise the request waits for a person.
- The install's notification modules can send **New requests** (Settings →
  Notifications).

**Following**: the bell on a series page (and the *Following* filter in the
library). New chapters of series you follow go to your own notifications.

**Your own notifications**: My account → *Notifications* → Add, and set up
ntfy, Discord, Telegram, Gotify, Apprise or a webhook that is yours alone.
You choose whether it gets new chapters of series you follow, news about your
requests, or both. These targets can only reach public addresses — for one
inside your network, ask an administrator to add it under Settings →
Notifications.

## 7. Reading in the browser

mangarr has its own reader, so a browser is enough: open a chapter from the
chapters table, the **Continue reading** shelf or the button on a series
page (`/read/<chapter>`). Chapters that aren't downloaded are streamed from
the source and queued, as in the apps.

- **Modes**: right to left, left to right, vertical, and **webtoon** (one long
  strip, side padding, gaps on or off).
- **Pages**: fit screen, width, height or original size; two-page spreads
  (with the cover alone) or single pages; **split double pages** into two, in
  reading order.
- **Crop borders** removes the uniform white or black margins around a page,
  measured on the server, so pages keep their quality.
- **Tap zones** as in Mihon (L-shaped, Kindle-style, edges, left/right, off,
  and inverted), keyboard (arrows, space, Page Up/Down, Home/End, `f` for
  full screen, `m` for the bars, Esc to leave) and swipes.
- Progress is saved as you read and shared with everything else; the last page
  finishes the chapter and the next one follows, after a card that says what
  comes next.
- Settings are kept **per series** (like Mihon), and *Use for all series*
  makes them your default. Everything is per account.

## 8. Single sign-on (optional)

Sign in with Authentik, Authelia, Keycloak, Pocket ID, Google or any other
OpenID Connect provider: **Settings → Single sign-on**.

1. Create an OAuth2/OpenID application at the provider with the **redirect
   URL** shown on the page (set Settings → General → *Public URL* first, so
   the address is right).
2. Fill in the issuer URL, client id and secret, turn it on and save. mangarr
   checks the issuer's discovery document when you save.
3. **Accounts**: *Make an account on first sign-in* lets anyone the provider
   lets through in; leave it off and people need an invite, which they can
   redeem with the provider's account. *Sign in to an account here with the
   same username* links existing accounts — only turn it on if you control
   usernames at the provider, since anyone who can pick one there could take
   over that account.
4. **Groups**: map the provider's groups to mangarr groups. People land in
   the first group that matches, at every sign-in, so the provider stays the
   source of truth. *Turn away people in none of these groups* keeps everyone
   else out. The last administrator never loses the role this way.
5. **Passwords**: turn *Keep password sign-in for everyone* off to make
   single sign-on the only way in. Administrators may still use a password,
   so you can get back in when the provider is down.
## 9. Readers and cleanup (optional)

Cleanup deletes chapters **every reader** has finished.

1. Settings → Readers → add each person, then **Link account** with *their
   own* Komga API key (Komga only exposes progress to the user itself).
   People with an account here can do it themselves under **My account →
   Library servers**, for their own reader.
2. Cleanup → enable, keep **Dry run** on at first, check the preview.
3. Defaults: ongoing series only, keep the last read chapter (keeps Mihon's
   tracker anchored), 7-day grace period, readers who never opened a series
   don't block it, `keep` tag excludes a series, files go to the recycle bin.
4. Cleaned chapters are never downloaded again; use **Restore** on a chapter
   to get it back.

Read progress arrives **live from Komga**: each linked account keeps Komga's
event stream open, so a chapter read on any device shows up in mangarr within
seconds (the Readers page shows *live*). Kavita is checked on a timer
(Settings → Readers, every 30 minutes by default). Each series shows how far
its readers got (*Continue: ch. N*, a read bar on the Series page, and a link
to open it in Komga).

## 10. Processing: upscaling and re-encoding (optional)

Processing is configured per profile (Settings → Profiles). By default it runs
**in the background**: chapters are imported as downloaded (readable right
away) and processed later, rewritten at the same path so Komga keeps read
progress. Background work runs one chapter at a time behind downloads; use
Settings → Schedule to pause it outside the night. When you turn processing on
for a profile, mangarr asks whether to process chapters you already have.

### Upscaling

Small pages look soft on an iPad. Profiles can upscale pages narrower than a
threshold (default 1400 px) with waifu2x / Real-CUGAN / Real-ESRGAN.

1. Give it a GPU. The full image (`:latest`, amd64) contains the upscalers:
   - **On the server itself** (e.g. the N100's iGPU): pass `/dev/dri` and the
     render group (`group_add`, see the compose example). mangarr then adds a
     *Built-in (this server)* upscaler automatically (enabled when a real GPU
     is visible).
   - **On another machine** (a desktop GPU): run the same image with
     `MANGARR_MODE=upscaler`, `MANGARR_SERVER_URL=http://<server>:8787` and
     `MANGARR_API_KEY`. It registers itself and shows up under Settings →
     Upscalers; it's preferred over the built-in one while online, and
     chapters simply wait while it's off. Set `MANGARR_NODE_URL` if the server
     can't reach it by host name (default `http://<hostname>:8788`). NVIDIA
     needs the container toolkit.
   - Without registration you can still add a *mangarr-upscaler* module by hand
     (URL + `MANGARR_UPSCALER_TOKEN`).
2. Benchmark your hardware and pick a model:
   ```bash
   scripts/upscale-bench.sh http://host:8788 <token> "/data/manga/en/Series/Series Ch.0001.cbz" 4
   ```
   Rough guidance: `realesr-animevideov3` is fastest (good for color
   webtoons), `waifu2x-cunet` cleans black & white manga well, `realcugan`
   is sharper and slower.
3. Settings → Profiles → enable upscaling, choose model and widths. If the
   upscaler is offline, chapters wait and are upscaled when it's back.

### Re-encoding (AVIF / JPEG XL)

| Format | Saves | Readers that can't open it |
|---|---|---|
| AVIF (lossy) | typically 40–70% | KOReader; Chunky only through Komga's OPDS (Komga converts); 32-bit ARM Komga |
| JPEG XL (lossless, JPEG pages only) | ~20%, reversible | Kavita, KOReader |

Mihon 0.17+, Tachimanga, Panels (iOS 17+) and Komga's official amd64/arm64
image read both. After the first re-encoded chapter mangarr asks Komga whether
it could read it; if not, re-encoding pauses with a health error until you
resume it (System → Status).

- Encoders: the full image includes `avifenc` and `cjxl` (fast). The slim image
  uses a built-in AVIF encoder that works everywhere but is much slower.
- Pages are kept as they are unless re-encoding saves at least the configured
  percentage; black-and-white pages are encoded without color.
- Try settings on your own pages: Profile → *Preview on a chapter* (shows the
  pages side by side and gives a sample CBZ to open on the iPad), or
  ```bash
  docker exec mangarr mangarr bench encode "/data/manga/en/Series/Series Ch.0001.cbz"
  ```
- Originals go to the recycle bin unless you turn that off (to free the space
  immediately).

## 11. Moving and renaming

- **Move a series** to another root folder or folder name: Edit on the series
  page, or select several on the Series page (*Select* → *Move…*). Files move
  in the background (copied, verified and then deleted when the destination is
  on another disk); downloads for that series wait meanwhile. Turn off *Move
  the files* when you already moved them by hand.
- **Move a whole root folder**: Settings → Media management → the folder icon
  on a root folder. Update library server path mappings afterwards.
- **Rename files** after changing the naming format: series page → *Rename
  files* (or several at once from the Series page), with a preview. Folders can
  follow title changes (Media management → *Rename a series folder when its
  title changes*).
- **Read progress**: moving files to another Komga/Kavita library (or renaming
  them) can reset progress there. mangarr keeps each reader's progress and
  writes it back once the server has scanned the new files (readers need linked
  accounts, see section 9). It never lowers progress on the server.

## 12. Importing from Mihon, Tachiyomi, Suwayomi or Aidoku

Import library → upload a backup:

- **Mihon, Tachiyomi and forks (J2K, SY, …), Suwayomi**: `.tachibk` or
  `.proto.gz` (Mihon: More → Backup and restore → Create backup; Suwayomi:
  Settings → Backup). Old Tachiyomi JSON backups aren't supported: restore
  them in Mihon and make a new backup.
- **Aidoku**: `.aib` (Settings → Backups).

Nothing is added until you start the import. mangarr first matches every
manga in the background:

- **Source**: Mihon/Suwayomi source ids are the same as Keiyoushi's, so a
  manga maps exactly to its catalog. When the catalog's extension isn't
  installed, *Install extensions* installs the missing ones and matches those
  manga again. Aidoku sources are matched by name; MangaDex links are converted
  directly, others are checked at the catalog or found by title. Anything
  unsure is marked *Needs review* with the best suggestion: *Accept* it or
  *Pick source* (the usual search).
- **Metadata**: AniList from the backup's tracker (MAL ids are converted
  through AniList), otherwise a confident title match. You can pick or remove
  it per manga.
- **Already in the library**: the source is added to the existing series and
  read chapters are merged. Manga that appear twice (the same series at two
  sources) become one series with both sources.

Options (per import): root folder and profile (overridable per category),
categories as tags, only library manga (not history), and **monitoring from
the first unread chapter**, so read chapters aren't downloaded again. Read
chapters are imported for a reader (a new "Mihon backup" reader by default;
pick your own to let cleanup use them). With *Mark them read in
Komga/Kavita*, chapters you read that get downloaded later are marked read on
your library server too. Scanlators you excluded in the app stay blocked for
that series (Edit series → *Blocked scanlators*).

Importing fetches each series' chapter list from its source, with the usual
request throttling, so a library of hundreds of series takes a while; the page
shows progress and can be left. Running an import again only picks up
entries that aren't imported yet (and retries failed ones).

## 13. PostgreSQL (optional)

mangarr starts on SQLite. To move to PostgreSQL (13 or newer), create an
empty database and a user for mangarr, then open **System → Database**:
enter the server's details, **Test connection**, then **Move data and
switch**. mangarr pauses downloads and tasks, copies everything (usually
seconds), and restarts on PostgreSQL. The address is kept in
`/config/database.dsn`. The SQLite file stays in `/config`; **Move to SQLite**
on the same page copies the data back.

If you set `MANGARR_DB` yourself, the page still copies the data, and then
tells you to change the variable and restart the container.

## 14. Logs, caches and bug reports

- **Logs** are written to `/config/logs/mangarr.txt` (rotated at 5 MB, 5 files
  kept; `MANGARR_LOG_DIR=off` disables them). System → Logs downloads them as a
  zip. API keys, passwords, tokens and module secrets are masked in the page,
  in copies and in downloads.
- **Download diagnostics** (System → Status) bundles logs, status, health,
  modules, settings and the queue for a bug report, masked the same way.
  Series titles and folder names are included, so look through it before
  posting it publicly.
- **Image cache**: thumbnails and covers are stored as 768px JPEGs (sources
  often send multi-megapixel originals, which also upset iOS Safari). The
  cache stays under Settings → General → *image cache limit* (512 MB by
  default) by removing the oldest images; System → Status shows its size and
  can *Compact* or clear it.

## 15. Backups & upgrades

Daily backups go to `/config/backups` (System → Backups). They hold the
whole database as a SQLite file on both SQLite and PostgreSQL installs, so a
backup restores into either, also on another install: **Add a backup file**,
then **Restore** (mangarr replaces its data and restarts; your library files
aren't touched). Suwayomi's own data is disposable: mangarr keeps the
real identity of every series (`sourceId` + URL) and re-links if Suwayomi's
database is lost. When upgrading Suwayomi, mangarr shows a health warning if
the version differs from the tested one.
