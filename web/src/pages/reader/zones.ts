import type { ReaderSettings } from "./settings";

export type TapAction = "prev" | "next" | "menu";

/**
 * tapAction maps a tap (x, y in 0..1 of the screen) to an action, like
 * Mihon's navigation layouts. Right-to-left mirrors the layout; invert
 * swaps previous and next.
 */
export function tapAction(x: number, y: number, s: ReaderSettings): TapAction {
  let zones = s.tapZones;
  if (zones === "auto") zones = s.mode === "webtoon" ? "kindle" : "lshape";
  if (zones === "off") return "menu";
  if (s.mode === "paged" && s.direction === "rtl") x = 1 - x;
  let a: TapAction = "menu";
  switch (zones) {
    case "lshape":
      if (x < 1 / 3 && y < 2 / 3) a = "prev";
      else if (y < 1 / 3 && x < 2 / 3) a = "prev";
      else if (x > 2 / 3 && y > 1 / 3) a = "next";
      else if (y > 2 / 3 && x > 1 / 3) a = "next";
      break;
    case "kindle":
      if (y > 1 / 3) a = x < 1 / 3 ? "prev" : "next";
      break;
    case "edge":
      if (x < 0.2) a = "prev";
      else if (x > 0.8) a = "next";
      break;
    case "leftright":
      if (x < 0.4) a = "prev";
      else if (x > 0.6) a = "next";
      break;
  }
  if (s.invertTaps && a !== "menu") a = a === "prev" ? "next" : "prev";
  return a;
}
