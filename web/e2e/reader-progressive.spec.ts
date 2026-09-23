import { expect, test } from "@playwright/test";

const image = (width: number, height: number) => `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><rect width="100%" height="100%" fill="#555"/></svg>`;

test("webtoon previews reserve each page's final height", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 700 });
  let releaseFull!: () => void;
  const fullReady = new Promise<void>((resolve) => { releaseFull = resolve; });
  const sizes: Record<number, [number, number]> = { 1: [600, 1200], 2: [600, 1800] };
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    const pageImage = path.match(/\/read\/chapters\/1\/pages\/(\d+)$/);
    if (pageImage) {
      const number = Number(pageImage[1]);
      if (url.searchParams.get("w") !== "320") await fullReady;
      const [width, height] = sizes[number];
      await route.fulfill({ contentType: "image/svg+xml", body: image(width, height) }).catch(() => undefined);
      return;
    }
    if (path.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
      return;
    }
    if (path.endsWith("/read/settings")) {
      await route.fulfill({ json: { defaults: { autoNext: false, direction: "vertical", keepAwake: false, mode: "webtoon", preload: 2 }, series: null } });
      return;
    }
    if (path.endsWith("/read/chapters/1") && route.request().method() === "GET") {
      await route.fulfill({ json: {
        id: 1, seriesId: 1, seriesTitle: "Preview Test", number: "1", readingDirection: "webtoon", downloaded: true, canDownload: false,
        pages: [1, 2].map((number) => ({ number, mediaType: "image/svg+xml" })),
        progress: { page: 0, completed: false },
      } });
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/read/1");
  const first = page.locator('[data-page="1"]');
  const second = page.locator('[data-page="2"]');
  await expect.poll(() => first.locator('img[aria-hidden]').evaluate((node: HTMLImageElement) => node.complete && node.naturalHeight > 0)).toBe(true);
  await expect.poll(() => second.locator('img[aria-hidden]').evaluate((node: HTMLImageElement) => node.complete && node.naturalHeight > 0)).toBe(true);
  const previewFirst = await first.evaluate((node) => node.getBoundingClientRect().height);
  const previewSecond = await second.evaluate((node) => node.getBoundingClientRect().height);
  expect(previewFirst).toBeCloseTo(1800, 0);
  expect(previewSecond).toBeCloseTo(2700, 0);

  releaseFull();
  await expect.poll(() => first.locator('img:not([aria-hidden])').evaluate((node: HTMLImageElement) => node.complete && node.naturalHeight > 0)).toBe(true);
  const loadedFirst = await first.evaluate((node) => node.getBoundingClientRect().height);
  expect(loadedFirst).toBeCloseTo(previewFirst, 0);
});
