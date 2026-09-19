# Mihon and Mangarr's Komga interface

Use the separate **Reading apps** address shown in Mangarr, rather than the web interface port. Configure one Komga extension instance with that address and a device API key from My account → Reading apps. Open a title through the extension, then enable Komga under Mihon's enhanced tracking services.

## One-file setup from a Mihon backup

For the primary **Komga** source instance, Mangarr can create a standard `.tachibk` containing the signed-in account's visible library, chapter list and current read/page progress. It also stores the Reading apps address and a newly generated API key in Mihon's per-source settings.

1. Install the Komga extension from the Keiyoushi repository in Mihon.
2. In Mangarr, open **My account → Reading apps → Mihon** and download the setup backup.
3. In Mihon, open **More → Backup and restore → Restore backup** and select the downloaded file.
4. Restore **Library entries** and **Source settings**. Mihon reconnects each entry by the Komga extension's stable source ID and its Mangarr series URL.
5. Refresh one restored title, then enable Komga under **Settings → Tracking** for completed-chapter tracking.

The export follows the account's group scope and never includes series that account cannot see. The file contains a credential, so each export creates a separately named device under **My account → Reading apps → Devices**. Revoke that device if the file is lost, shared or replaced. Revoking it invalidates the embedded key without affecting other phones or readers. The backup does not contain Mangarr passwords, downloaded page files or credentials for other apps.

## Tracking session compatibility

The [Komga extension](https://github.com/keiyoushi/extensions-source/blob/main/src/all/komga/src/eu/kanade/tachiyomi/extension/all/komga/Komga.kt) sends `X-API-Key` on catalog requests. [Mihon's Komga tracker](https://github.com/mihonapp/mihon/blob/main/app/src/main/java/eu/kanade/tachiyomi/data/track/komga/KomgaApi.kt) uses its shared cookie jar for series details and `/api/v2/series/{id}/read-progress/tachiyomi`, without forwarding that API key.

Mangarr now validates the cookie's signature, expiry and owning key/account before deciding whether a successful key-authenticated request needs to renew it. Previously, any cookie with the expected name prevented renewal, including expired cookies and cookies for another device key. This could leave catalog browsing working while tracker requests failed authentication. After updating, refresh the title through the Komga extension so it receives a fresh session, then retry tracking.

Completed chapters sync through enhanced tracking; page-level progress uses the reading-app API or a restored backup. The v2 tracker uses actual chapter numbers, including zero and fractions; v1 uses chapter positions. Both preserve gaps and avoid lowering existing progress.

## Validation status

HTTP integration tests replay the extension's chapter-list request followed by cookie-only tracker detail, progress and update requests. They cover missing, malformed, expired, near-expiry and different-device sessions on SQLite and PostgreSQL, plus chapter zero, fractional numbers and gaps. Existing Komga catalog/progress/authentication tests also pass.

A real Mihon session has not yet been tested: no Android device or configured emulator was available in the local environment. The original “no chapter found” report is therefore not confirmed resolved end to end; the session-renewal defect is independently reproduced and covered.
