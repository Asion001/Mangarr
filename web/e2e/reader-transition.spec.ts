import { expect, test, type Page } from "@playwright/test";

const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="700" height="1000"><rect width="100%" height="100%" fill="#333"/></svg>';

const chapter = (id: number, next?: { id: number; number: string }) => ({
  id, seriesId: 7, seriesTitle: "Divider Test", number: String(id), readingDirection: "ltr", downloaded: true, canDownload: false,
  pages: Array.from({ length: 3 }, (_, index) => ({ number: index + 1, mediaType: "image/svg+xml" })),
  progress: { page: 0, completed: false },
  prev: id > 1 ? { id: id - 1, number: String(id - 1) } : undefined,
  next,
});

async function mock(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (/\/read\/chapters\/\d+\/pages\/\d+$/.test(path)) return route.fulfill({ contentType: "image/svg+xml", body: svg });
    if (path.endsWith("/auth/status")) return route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
    if (path.endsWith("/read/settings")) return route.fulfill({ json: { defaults: { direction: "ltr", mode: "paged", spread: "single" }, series: null } });
    if (route.request().method() === "GET" && path.endsWith("/read/chapters/1")) return route.fulfill({ json: chapter(1, { id: 2, number: "2" }) });
    if (route.request().method() === "GET" && path.endsWith("/read/chapters/2")) return route.fulfill({ json: chapter(2) });
    return route.fulfill({ json: {} });
  });
}

// the right-hand "next" tap zone
const tapNext = (page: Page) => {
  const vp = page.viewportSize()!;
  return page.mouse.click(vp.width * 0.9, vp.height * 0.6);
};

test("the page-turn tap carries on through the chapter divider", async ({ page }) => {
  await mock(page);
  await page.goto("/read/1?page=last");
  await expect(page.getByText("3 / 3")).toBeVisible();
  await tapNext(page);
  await expect(page.getByText("Finished")).toBeVisible();
  await tapNext(page);
  await expect(page).toHaveURL(/\/read\/2$/);
});

test("the last chapter's divider leads back to the series", async ({ page }) => {
  await mock(page);
  await page.goto("/read/2?page=last");
  await expect(page.getByText("3 / 3")).toBeVisible();
  await tapNext(page);
  await expect(page.getByText("There's no next chapter yet.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Back to the last page" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Back to the series" }).filter({ hasText: "Back to the series" })).toHaveAttribute("href", /\/series\/7$/);
  await tapNext(page);
  await expect(page).toHaveURL(/\/series\/7$/);
});
