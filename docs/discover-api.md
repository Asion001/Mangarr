# Discover shelf API

`GET /api/v1/discover` retains its existing response and limits. The additional
`GET /api/v1/discover/{shelf}` accepts `recommendations`, `recently-updated` or
`popular`. It requires the same signed-in access as Discover. No web pages or
preference persistence are added by this API change.

Responses contain `library` (existing `DiscoverLibraryItem` cards), `popular`
(existing `DiscoverSourceItem` cards), `sourceErrors`, and optional `nextCursor`.
Only the array for the requested shelf contains cards. Source cards retain the
module/source/URL/engine identity and signed thumbnail; local cards retain the
series ID and reader counts. Existing Add, Request and Read endpoints retain
their own permissions.

## Queries

| Query | Library shelves | Popular |
| --- | --- | --- |
| `lang` | Exact language, case-insensitive | Catalog language; includes multi-language catalogs, defaults to configured languages |
| `genre`, `tag` | Exact metadata genre/tag, case-insensitive | Unsupported |
| `tagId` | Library tag ID | Unsupported |
| `format` | `manga`, `manhwa`, `manhua` | Unsupported |
| `status` | `unknown`, `ongoing`, `completed`, `hiatus`, `cancelled` | Unsupported |
| `source` | Linked catalog key `moduleId:sourceId` | Narrows active, prioritized catalogs to that key |
| `inLibrary` | `true` keeps all candidates; `false` returns none | `true`/`false` match membership in the caller's visible library |
| `rootFolderId` | Restricts series | Restricts membership matching and selects catalog priorities |
| `sort` | See below | `popularity` (default), `recently-updated` |
| `pageSize` | 1–100, default 50 | 1–100, default 50 |
| `cursor` | Opaque continuation | Opaque continuation |

Library sorts are `recently-updated` (series/chapter modification time), `newest`
(library added time), and `title` (case-insensitive ascending). Recommendations
also supports `recommended`, its default personalized genre/follow ranking.
Recently updated defaults to `recently-updated`. Ties use title and series ID.
Library popularity is unsupported because no popularity metric is stored.
Library shelves require at least one chapter, and recommendations excludes
series the caller has started.

Popular traverses catalogs in priority order, using each provider's Popular or
Latest pages. Catalogs without Latest support are reported in `sourceErrors`
and skipped for `recently-updated` sorting. It does not impose a global rank
across catalogs. Source browse
responses lack genre/tag, format, status and dates and cannot request global
newest/title ordering. Unsupported filter/sort combinations return **400**;
invalid enum values and page sizes return **422**.

The existing `HideNSFW` source setting always applies, with no query override.
As with the combined Discover endpoint, it hides source catalogs, not local
library metadata. Library visibility follows account root/tag scope, including
the existing manager/admin bypass. Popular membership uses visible title and
alternative-title matches (including series without chapters); a hidden series
never contributes its ID or causes `inLibrary=true` to match. Catalog language
selection does not limit membership matching to that language.

## Continuation and stability

Start, for example, with
`/api/v1/discover/recommendations?genre=Adventure&sort=newest&pageSize=25`.
Repeat the filters and sort with the returned `cursor`; `pageSize` may change.
Stop when `nextCursor` is absent. There is no total count.

Cursors keep immutable state in the existing bounded memory cache for up to
15 minutes. They are bound to the caller, permissions, normalized query and
catalog generation. A mismatched cursor returns **400**. Expiration, eviction
or restart returns **410**; restart without a cursor. Cache capacity failure
returns **503** rather than silently truncating the shelf.

Library order is snapshotted as series IDs. Inserts and sort-field changes do
not shift the traversal. Each page reevaluates visibility, filters and current
reader counts, so deleted, hidden or newly started recommendations can disappear.
This reuses the existing reading service's full library aggregation; it does
not yet provide SQL-level paging of that aggregation. Latest chapter details
are loaded only for the returned recently-updated page.

Popular retains unconsumed source page entries and deduplicates normalized
titles across the traversal. Provider page numbers still mean upstream
reordering can omit titles between fetched pages. Each request fetches at most
eight source pages with an eight-second browse budget. Filtering can produce an
empty response with a continuation; clients must follow `nextCursor` even then.
A failed catalog appears in `sourceErrors` and is skipped so other catalogs can
still contribute. Restart the shelf to retry failed catalogs.
