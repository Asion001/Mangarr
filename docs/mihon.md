# Reading in Mihon

Mihon reads mangarr through the Komga extension, pointed at mangarr's
Komga-compatible API. An administrator turns that on first under **Settings →
Reading apps** (see [setup §5](setup.md#5-reading-apps)); use the **Reading
apps** address shown there, not the web interface's port.

## Quickest: set it up from a backup

mangarr can write a standard Mihon backup (`.tachibk`) with your library,
chapter lists and progress, and the Komga extension already configured with
the server address and a new device key.

1. In Mihon, install the **Komga** extension from the Keiyoushi repository.
2. In mangarr, open **My account → Reading apps → Set up Mihon from a
   backup** and download the file.
3. In Mihon, open **More → Backup and restore → Restore backup** and pick the
   file. Restore **Library entries** and **Source settings**.
4. Refresh one title, then turn on **Komga** under Mihon's **Settings →
   Tracking** (enhanced services) so finished chapters sync back.

The backup holds only the series your group can see. It contains a
credential, so every download creates its own device under **My account →
Reading apps → Devices**: revoke that device if the file is lost, shared or
replaced, and other phones keep working. It holds no mangarr password, no
page files and no credentials for anything else.

## By hand

1. Add a device under **My account → Reading apps** and copy its API key (it
   is shown once).
2. In Mihon, open the Komga extension's settings and enter the Reading apps
   address and the key.
3. Open a title through the extension, then turn on **Komga** under
   **Settings → Tracking**.

## How progress syncs

- **Finished chapters** sync through Mihon's Komga tracker (enhanced
  tracking). The tracker uses real chapter numbers, including chapter 0 and
  fractions like 10.5, and never lowers progress that is already further.
- **Page-level progress** comes from the reading-app API or a restored backup.
- mangarr passes every change on to Komga and Kavita, so their own readers
  stay up to date too.

## Troubleshooting

Mihon's Komga tracker doesn't send the API key: it relies on the session
cookie the extension's requests left behind. mangarr renews that cookie on
every key-authenticated request when it is missing, expired, about to
expire or belongs to another device. If tracking fails while browsing works,
refresh the title through the Komga extension so it gets a fresh session,
then retry tracking.

Background on the protocol: the
[Komga extension](https://github.com/keiyoushi/extensions-source/blob/main/src/all/komga/src/eu/kanade/tachiyomi/extension/all/komga/Komga.kt)
and [Mihon's Komga tracker](https://github.com/mihonapp/mihon/blob/main/app/src/main/java/eu/kanade/tachiyomi/data/track/komga/KomgaApi.kt).
