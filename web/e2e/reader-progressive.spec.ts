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

test("crop waits for measured bounds and replaces an earlier natural size", async ({ page }) => {
  let releaseBounds!: () => void;
  let boundsAsked = false;
  const boundsReady = new Promise<void>((resolve) => { releaseBounds = resolve; });
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path.match(/\/read\/chapters\/1\/pages\/1$/)) {
      await route.fulfill({ contentType: "image/svg+xml", body: image(400, 600) });
      return;
    }
    if (path.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
      return;
    }
    if (path.endsWith("/read/settings")) {
      await route.fulfill({ json: { defaults: { autoNext: false, crop: true, direction: "ltr", keepAwake: false, mode: "paged", preload: 1, scale: "screen", spread: "single" }, series: null } });
      return;
    }
    if (path.endsWith("/read/chapters/1/bounds")) {
      boundsAsked = true;
      await boundsReady;
      await route.fulfill({ json: [{ number: 1, width: 400, height: 600, x: 40, y: 60, w: 320, h: 480 }] });
      return;
    }
    if (path.endsWith("/read/chapters/1") && route.request().method() === "GET") {
      await route.fulfill({ json: {
        id: 1, seriesId: 1, seriesTitle: "Crop Test", number: "1", readingDirection: "ltr", downloaded: true, canDownload: false,
        pages: [{ number: 1, mediaType: "image/svg+xml" }],
        progress: { page: 0, completed: false },
      } });
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/read/1");
  const readerImage = page.locator('img[src*="/read/chapters/1/pages/1"]').first();
  await expect.poll(() => boundsAsked).toBe(true);
  await expect.poll(() => readerImage.evaluate((node: HTMLImageElement) => node.complete && node.naturalWidth === 400)).toBe(true);
  await expect(readerImage).toBeHidden();

  releaseBounds();
  await expect(readerImage).toBeVisible();
  await expect.poll(() => readerImage.evaluate((node) => node.style.width)).toBe("125%");
  expect(await readerImage.evaluate((node) => ({ left: node.style.left, top: node.style.top }))).toEqual({ left: "-12.5%", top: "-12.5%" });
});
