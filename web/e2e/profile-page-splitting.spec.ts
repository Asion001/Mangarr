import { expect, test } from "@playwright/test";

test("an older profile can enable tall-page splitting and save its ratios", async ({ page }) => {
  const profile = {
    id: 1,
    name: "Default",
    isDefault: true,
    createdAt: "2026-09-24T00:00:00Z",
    updatedAt: "2026-09-24T00:00:00Z",
    config: {
      preferredScanlators: [], blockedScanlators: [], allowUpgrades: false, minPages: 0,
      upscale: { enabled: false, upscalerId: 0, minWidth: 1400, maxWidth: 2048, model: "waifu2x-cunet", noise: 1, format: "source", quality: 90 },
      encode: { format: "keep", preset: "balanced", quality: 0, speed: 0, grayscale: true, progressive: false, minSavingsPct: 10, recycleOriginals: true },
      processTiming: "background", processExisting: false, cleanup: {},
    },
  };
  let saved: Record<string, any> | undefined;
  await page.addInitScript(() => localStorage.setItem("mangarr:ui:anonymous:0", JSON.stringify({ locale: "en", mode: "editing" })));
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/auth/status")) return route.fulfill({ json: { authenticated: true, authDisabled: true, account: { kind: "anonymous", id: 0, permissions: ["admin"] } } });
    if (path.endsWith("/profiles") && request.method() === "GET") return route.fulfill({ json: [profile] });
    if (path.endsWith("/profiles/1") && request.method() === "PUT") {
      saved = request.postDataJSON();
      return route.fulfill({ json: saved });
    }
    if (path.endsWith("/profiles/1/process-estimate")) return route.fulfill({ json: { files: 0, bytes: 0 } });
    if (path.endsWith("/settings/cleanup")) return route.fulfill({ json: {} });
    return route.fulfill({ json: {} });
  });

  await page.goto("/settings/profiles");
  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByRole("button", { name: "Page processing" }).click();
  await page.getByRole("switch", { name: "Split tall pages" }).click();
  await page.getByPlaceholder("3", { exact: true }).fill("4");
  await page.getByPlaceholder("2", { exact: true }).fill("1.8");
  await expect(page.getByText("Split strips over 4× width").first()).toBeVisible();
  await page.getByRole("button", { name: "Save", exact: true }).click();

  await expect.poll(() => saved?.config?.pages).toEqual({ junkUnder: 0, removeJunk: false, maxWidth: 0, splitTall: true, splitRatio: 4, segmentRatio: 1.8 });
});
