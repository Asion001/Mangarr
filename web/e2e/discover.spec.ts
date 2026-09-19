import { expect, test, type Page } from "@playwright/test";

async function mockDiscover(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, account: { kind: "local", id: 7, username: "reader", permissions: ["requests.create"] } } });
      return;
    }
    if (path.endsWith("/discover")) {
      await route.fulfill({
        json: {
          generatedAt: "2026-09-19T12:00:00Z",
          popularCached: false,
          recommendations: [{ seriesId: 1, title: "Recommended One", language: "en", status: "ongoing", books: 10, unread: 4, changedAt: "2026-09-19T11:00:00Z", coverUrl: "", genres: ["Drama"], matchingGenres: ["Drama"], reason: "matches-genres" }],
          updates: [{ seriesId: 2, title: "Updated One", language: "uk", status: "ongoing", books: 8, unread: 1, changedAt: "2026-09-19T10:00:00Z", coverUrl: "", genres: [], latestChapter: "8", reason: "recent-update" }],
          popular: [{ moduleId: 4, moduleName: "source", sourceId: "catalog", sourceName: "Catalog", title: "Popular One", language: "ru", url: "/popular-one" }],
          sourceErrors: [{ source: "slow", name: "Slow source", error: "timed out" }],
        },
      });
      return;
    }
    if (path.endsWith("/reading/shelf")) {
      await route.fulfill({ json: { reader: "reader", items: [] } });
      return;
    }
    await route.fulfill({ json: {} });
  });
}

test("reader Discover page combines recommendations, updates and source titles", async ({ page }) => {
  await mockDiscover(page);
  await page.goto("/discover");

  await expect(page.getByRole("heading", { name: "Discover", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Discover", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Recommendations" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open series: Recommended One" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Recently updated" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Popular from your sources" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Request: Popular One" })).toHaveAttribute("href", "/requests?tab=ask&q=Popular%20One");
  await expect(page.getByText("Some sources are unavailable")).toBeVisible();
});
