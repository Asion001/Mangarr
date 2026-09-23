import { expect, test, type Page } from "@playwright/test";

async function mockEvents(page: Page) {
  await page.addInitScript(() => {
    class MockEventSource extends EventTarget {
      static sources: MockEventSource[] = [];
      onopen: ((event: Event) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      closed = false;
      readyState = 0;

      constructor(_url: string | URL) {
        super();
        MockEventSource.sources.push(this);
        setTimeout(() => { if (!this.closed) this.open(); }, 0);
      }

      open() { this.readyState = 1; this.onopen?.(new Event("open")); }
      fail(gaveUp = false) { this.readyState = gaveUp ? 2 : 0; this.onerror?.(new Event("error")); }
      close() { this.closed = true; this.readyState = 2; }
    }

    Object.defineProperty(window, "EventSource", { value: MockEventSource });
    Object.defineProperty(window, "__liveTest", {
      value: {
        emit(type: string, data: unknown) {
          for (const source of MockEventSource.sources.filter((item) => !item.closed)) {
            source.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }));
          }
        },
        fail(gaveUp = false) {
          for (const source of MockEventSource.sources.filter((item) => !item.closed)) source.fail(gaveUp);
        },
        count() {
          return MockEventSource.sources.length;
        },
        open() {
          for (const source of MockEventSource.sources.filter((item) => !item.closed)) source.open();
        },
      },
    });
  });
}

test("updates refresh on chapter events and after an SSE reconnect", async ({ page }) => {
  await mockEvents(page);

  let version = 1;
  let requests = 0;
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
      return;
    }
    if (path.endsWith("/updates")) {
      requests++;
      const items = [
        { kind: "chapter", at: "2026-09-23T08:00:00Z", seriesId: 1, seriesTitle: "First title", chapterId: 11, number: "1", coverUrl: "", readable: true, readState: "unread" },
      ];
      if (version >= 2) items.unshift({ kind: "chapter", at: "2026-09-23T09:00:00Z", seriesId: 2, seriesTitle: "Event title", chapterId: 22, number: "2", coverUrl: "", readable: true, readState: "unread" });
      if (version >= 3) items.unshift({ kind: "chapter", at: "2026-09-23T10:00:00Z", seriesId: 3, seriesTitle: "Reconnect title", chapterId: 33, number: "3", coverUrl: "", readable: true, readState: "unread" });
      await route.fulfill({ json: { items, total: items.length, page: 1, pageSize: 50 } });
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/updates");
  await expect(page.getByText("First title")).toBeVisible();

  version = 2;
  await page.evaluate(() => (window as unknown as { __liveTest: { emit: (type: string, data: unknown) => void } }).__liveTest.emit("resource.changed", { payload: { name: "chapter" } }));
  await expect(page.getByText("Event title")).toBeVisible();
  await expect(page.getByText("First title")).toHaveCount(1);

  version = 3;
  await page.evaluate(() => (window as unknown as { __liveTest: { fail: () => void } }).__liveTest.fail());
  await expect(page.getByRole("status")).toHaveText("reconnecting");
  await page.evaluate(() => window.dispatchEvent(new Event("offline")));
  await expect(page.getByRole("status")).toHaveText("offline");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.getByRole("status")).toHaveText("reconnecting");
  await page.evaluate(() => (window as unknown as { __liveTest: { open: () => void } }).__liveTest.open());
  await expect(page.getByText("Reconnect title")).toBeVisible();
  await expect(page.getByText("First title")).toHaveCount(1);
  expect(requests).toBeGreaterThanOrEqual(3);
});

test("the live stream starts again after the browser gives up on it", async ({ page }) => {
  await mockEvents(page);
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
      return;
    }
    await route.fulfill({ json: path.endsWith("/updates") ? { items: [], total: 0, page: 1, pageSize: 50 } : {} });
  });
  type Live = { __liveTest: { fail: (gaveUp?: boolean) => void; count: () => number } };
  await page.goto("/updates");
  await expect(page.getByRole("status")).toHaveCount(0);
  const before = await page.evaluate(() => (window as unknown as Live).__liveTest.count());
  // an error response (a proxy's 502 during a restart) closes an EventSource for good
  await page.evaluate(() => (window as unknown as Live).__liveTest.fail(true));
  await expect(page.getByRole("status")).toHaveText("reconnecting");
  await expect.poll(() => page.evaluate(() => (window as unknown as Live).__liveTest.count())).toBe(before + 1);
  await expect(page.getByRole("status")).toHaveCount(0);
});

test("updates pages forward with an opaque cursor and back from local history", async ({ page }) => {
  await mockEvents(page);
  const cursors: (string | null)[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/auth/status")) {
      await route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: [] } } });
      return;
    }
    if (url.pathname.endsWith("/updates")) {
      const cursor = url.searchParams.get("cursor");
      cursors.push(cursor);
      if (cursor) {
        await route.fulfill({ json: { items: [{ kind: "series", at: "2026-09-22T08:00:00Z", seriesId: 99, seriesTitle: "Cursor title", coverUrl: "", languages: [] }], total: 51, page: 1, pageSize: 50 } });
      } else {
        const items = Array.from({ length: 50 }, (_, index) => ({ kind: "series", at: "2026-09-23T08:00:00Z", seriesId: index + 1, seriesTitle: `First page ${index + 1}`, coverUrl: "", languages: [] }));
        await route.fulfill({ json: { items, total: 51, page: 1, pageSize: 50, nextCursor: "next-cursor" } });
      }
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/updates");
  await expect(page.getByText("First page 1", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByText("Cursor title")).toBeVisible();
  expect(cursors).toContain("next-cursor");
  await page.getByRole("button", { name: "Previous" }).click();
  await expect(page.getByText("First page 1", { exact: true })).toBeVisible();
});
