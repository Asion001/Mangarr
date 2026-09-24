import { expect, test, type Page } from "@playwright/test";

const engine = { id: 7, kind: "source", implementation: "suwayomi", name: "Suwayomi", enabled: true, priority: 0, settings: {}, capabilities: ["extensions", "browse", "preferences"] };

const catalog = (id: string, name: string, lang: string, priority: number, enabled = true, extension = "") => ({
  moduleId: 7, moduleName: "Suwayomi", id, name, lang, displayName: `${name} (${lang.toUpperCase()})`, supportsLatest: true, nsfw: false,
  enabled, priority, hidden: false, throttle: {}, extension,
});

type Put = Record<string, { enabled?: boolean; priority?: number }>;

async function mock(page: Page) {
  const catalogs = [
    catalog("wc", "WeebCentral", "en", 10),
    catalog("md-en", "MangaDex", "en", 20, true, "eu.kanade.md"),
    catalog("ml", "MangaLib", "ru", 30),
    catalog("off", "Asura", "en", 40, false),
  ];
  let installed = false;
  const puts: Put[] = [];
  await page.addInitScript(() => localStorage.setItem("mangarr:ui:anonymous:0", JSON.stringify({ locale: "en", mode: "editing" })));
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path.endsWith("/auth/status")) return route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: ["admin"] } } });
    if (path.endsWith("/modules")) return route.fulfill({ json: [engine] });
    if (path.endsWith("/catalogs/health"))
      return route.fulfill({ json: [{ moduleId: 7, sourceId: "wc", series: 12, failing: 3, lastSuccessAt: "2026-09-22T10:00:00Z" }, { moduleId: 7, sourceId: "md-en", series: 4, failing: 0 }] });
    if (path.endsWith("/catalogs") && req.method() === "PUT") {
      const body = req.postDataJSON() as Put;
      puts.push(body);
      for (const [k, p] of Object.entries(body)) {
        const c = catalogs.find((x) => `${x.moduleId}:${x.id}` === k);
        if (c) Object.assign(c, p);
      }
      return route.fulfill({ json: { generation: 1, items: catalogs, errors: [] } });
    }
    if (path.endsWith("/catalogs")) return route.fulfill({ json: { generation: 1, items: catalogs, errors: [] } });
    if (path.endsWith("/source-priorities")) return route.fulfill({ json: [] });
    if (path.endsWith("/settings/sources")) return route.fulfill({ json: { hideNsfw: true, defaultLanguages: ["en"] } });
    if (path.endsWith("/stores")) return route.fulfill({ json: ["https://example.test/index.min.json"] });
    if (path.endsWith("/extensions/eu.kanade.comick/install")) {
      installed = true;
      catalogs.push(catalog("ck-en", "Comick", "en", 50, true, "eu.kanade.comick"), catalog("ck-ru", "Comick", "ru", 60, true, "eu.kanade.comick"), catalog("ck-ja", "Comick", "ja", 70, true, "eu.kanade.comick"));
      return route.fulfill({ json: {} });
    }
    if (path.endsWith("/extensions"))
      return route.fulfill({
        json: [
          { pkg: "eu.kanade.md", name: "MangaDex", lang: "all", installed: true, hasUpdate: true, versionName: "1.4.201", versionCode: 1, nsfw: false, obsolete: false, iconUrl: "" },
          { pkg: "eu.kanade.comick", name: "Comick", lang: "all", installed, hasUpdate: false, versionName: "1.4.77", versionCode: 1, nsfw: false, obsolete: false, iconUrl: "" },
          { pkg: "eu.kanade.ja", name: "JapanOnly", lang: "ja", installed: false, hasUpdate: false, versionName: "1.0", versionCode: 1, nsfw: false, obsolete: false, iconUrl: "" },
        ],
      });
    if (path.endsWith("/series") || path.endsWith("/rootfolders")) return route.fulfill({ json: [] });
    return route.fulfill({ json: {} });
  });
  return puts;
}

test("my catalogs shows health, keeps the off ones apart and renumbers the whole list", async ({ page }) => {
  const puts = await mock(page);
  await page.goto("/sources");
  const rows = page.getByRole("region", { name: "Catalogs that are on" }).locator("ol > li");
  await expect(rows).toHaveCount(3);
  await expect(rows.first()).toContainText("3 of 12 links failing");
  await expect(rows.nth(2)).toContainText("Not used yet");
  await expect(page.getByRole("button", { name: /Off/ })).toContainText("1 installed catalogs");

  await rows.nth(2).getByRole("button", { name: /More for MangaLib/ }).click();
  await page.getByRole("menuitem", { name: "Move to top" }).click();
  await expect(rows.first()).toContainText("MangaLib");
  // every catalog of the engine gets a distinct priority, the one that's off last
  expect(puts.at(-1)).toEqual({ "7:ml": { priority: 10 }, "7:wc": { priority: 20 }, "7:md-en": { priority: 30 } });
});

test("add catalogs lists updates first and asks which languages to turn on after installing", async ({ page }) => {
  const puts = await mock(page);
  await page.goto("/sources/extensions"); // the old address still works
  await expect(page).toHaveURL(/\/sources\/add$/);
  await expect(page.getByRole("button", { name: /Add catalogs/ })).toContainText("1 updates");
  const updates = page.getByRole("region", { name: /Updates/ });
  await expect(updates).toContainText("MangaDex");
  // the search languages (en) filter the list: the Japanese-only extension is hidden
  await expect(page.getByText("JapanOnly")).toHaveCount(0);

  await page.getByRole("button", { name: "Install" }).click();
  const dialog = page.getByRole("dialog", { name: "Turn on which Comick catalogs?" });
  await expect(dialog.getByRole("checkbox", { name: "Comick (EN)" })).toBeChecked();
  await expect(dialog.getByRole("checkbox", { name: "Comick (RU)" })).not.toBeChecked();
  await dialog.getByRole("button", { name: "Turn on 1" }).click();
  expect(puts.at(-1)).toEqual({ "7:ck-en": { enabled: true }, "7:ck-ru": { enabled: false }, "7:ck-ja": { enabled: false } });
});
