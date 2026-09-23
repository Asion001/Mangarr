import { expect, test } from "@playwright/test";

const series = {
  id: 1, workId: 1, title: "Long Series", sortTitle: "long series", status: "ongoing", monitored: true, monitorNew: "all",
  rootFolderId: 1, path: "Long Series", profileId: 1, language: "en", sourcePriorityMode: "inherit", readingDirection: "ltr",
  tags: [], metadata: {}, addOptions: { pending: false }, addedAt: "2026-09-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z",
  coverUrl: "", following: false, sources: [], editions: [],
  stats: { chapterCount: 250, monitoredCount: 250, fileCount: 0, missingCount: 250, cleanedCount: 0, sizeOnDisk: 0, spaceSaved: 0, lastChapter: 250, readCount: 0, inProgressCount: 0 },
};

test("large chapter tables render a small remembered page while bulk selection still covers all", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("mangarr:ui:anonymous:0", JSON.stringify({ locale: "en", mode: "editing" })));
  const chapters = Array.from({ length: 250 }, (_, index) => ({
    id: index + 1, seriesId: 1, number: String(index + 1), numberSort: index + 1, title: `Chapter ${index + 1}`,
    monitored: true, releaseDate: "2026-09-01T00:00:00Z", state: "missing", releases: [], readBy: [],
  }));
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/status")) return route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: ["library.manage"] } } });
    if (path.endsWith("/series/1/chapters")) return route.fulfill({ json: chapters });
    if (path.endsWith("/series/1")) return route.fulfill({ json: series });
    if (path.endsWith("/series")) return route.fulfill({ json: [series] });
    if (path.endsWith("/queue")) return route.fulfill({ json: { total: 0, state: { paused: false }, items: [] } });
    if (path.endsWith("/reading/shelf")) return route.fulfill({ json: { items: [] } });
    if (path.endsWith("/health")) return route.fulfill({ json: { checks: [] } });
    return route.fulfill({ json: [] });
  });

  await page.goto("/series/1");
  const table = page.getByRole("table");
  await expect(page.getByText("Chapters (250)")).toBeVisible();
  await expect(table.locator("tbody > tr")).toHaveCount(25);
  await table.locator('thead input[type="checkbox"]').check();
  await expect(page.getByText("250 selected")).toBeVisible();

  const pageSize = page.getByRole("combobox", { name: "per page" });
  await pageSize.selectOption("50");
  await expect(table.locator("tbody > tr")).toHaveCount(50);
  await page.reload();
  await expect(pageSize).toHaveValue("50");
  await expect(page.getByRole("table").locator("tbody > tr")).toHaveCount(50);
});
