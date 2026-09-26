import { describe, expect, it } from "vitest";
import { parseShelfFilters, restoreShelfFilters, shelfParams, shelfSorts, shouldLoadShelfPage, type Shelf } from "./shelfState";

describe("Discover filter state", () => {
  it.each(["recommendations", "recently-updated", "popular"] as Shelf[])("uses only supported sorts for %s", (shelf) => {
    for (const sort of ["recommended", "popularity", "recently-updated", "newest", "title", "invalid"]) {
      const expected = shelfSorts[shelf].find((value) => value === sort) ?? shelfSorts[shelf][0];
      expect(parseShelfFilters(shelf, new URLSearchParams({ sort })).sort).toBe(expected);
    }
  });

  it("drops unsupported popular filters, paging state and invalid values", () => {
    const params = new URLSearchParams("genre=Adventure&tag=Quest&tagId=3&format=manga&status=ongoing&sort=title&lang=uk&source=4:catalog&inLibrary=false&cursor=expired&pageSize=100");
    expect(parseShelfFilters("popular", params)).toEqual({ sort: "popularity", lang: "uk", source: "4:catalog", inLibrary: "false" });
    expect(shelfParams(parseShelfFilters("recommendations", new URLSearchParams("source=bad&tagId=-2&rootFolderId=1.5&format=other&status=other&inLibrary=yes"))).toString()).toBe("sort=recommended");
  });

  it("round trips every library filter without retaining a cursor", () => {
    const params = new URLSearchParams("sort=newest&lang=ru&genre=Moon%20travel&tag=Space&tagId=3&format=manhwa&status=hiatus&source=4:catalog&inLibrary=true&rootFolderId=2&cursor=old");
    const filters = parseShelfFilters("recommendations", params);
    expect(filters).toEqual({ sort: "newest", lang: "ru", genre: "Moon travel", tag: "Space", tagId: 3, format: "manhwa", status: "hiatus", source: "4:catalog", inLibrary: "true", rootFolderId: 2 });
    expect(parseShelfFilters("recommendations", shelfParams(filters))).toEqual(filters);
  });

  it("restores saved choices only on bare URLs and makes defaults shareable", () => {
    const saved = "sort=newest&lang=ru&genre=Adventure";
    expect(restoreShelfFilters("recommendations", "", saved).lang).toBe("ru");
    const shared = restoreShelfFilters("recommendations", "sort=title", saved);
    expect(shared.lang).toBeUndefined();
    expect(shared.sort).toBe("title");
    const defaults = shelfParams(parseShelfFilters("recommendations", new URLSearchParams())).toString();
    expect(restoreShelfFilters("recommendations", defaults, saved).lang).toBeUndefined();
  });

  it("does not automatically retry failures or overlap pending pages", () => {
    expect(shouldLoadShelfPage(true, false, false)).toBe(true);
    expect(shouldLoadShelfPage(true, true, false)).toBe(false);
    expect(shouldLoadShelfPage(true, false, true)).toBe(false);
    expect(shouldLoadShelfPage(false, false, false)).toBe(false);
  });
});
