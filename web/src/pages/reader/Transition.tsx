import { t as tr, t } from "../../lib/i18n/core";
import { Link } from "react-router";
import type { S } from "../../api/client";

type Chapter = S["ReadChapter"];

const label = (c: { number: string; title?: string }) => `Ch. ${c.number}${c.title && c.title !== c.number && !c.title.endsWith(c.number) ? ` · ${c.title}` : ""}`;

/**
 * Transition is the screen between chapters (Mihon's "Finished / Next").
 * Taps and swipes outside its buttons fall through to the viewer, so the
 * page-turn gestures carry on into the next chapter.
 */
export function Transition({ edge, chapter, onGo, onBack }: { edge: "start" | "end"; chapter: Chapter; onGo: () => void; onBack: () => void }) {
  const other = edge === "end" ? chapter.next : chapter.prev;
  const btn = "rounded-md px-3 py-2 text-sm";
  return (
    <div className="flex h-full w-full items-center justify-center p-6">
      <div className="flex max-w-sm flex-col gap-4 text-center text-fg">
        <div>
          <div className="text-xs uppercase tracking-wide text-muted">{edge === "end" ? tr("Finished") : tr("Current")}</div>
          <div className="text-lg font-semibold">{label(chapter)}</div>
        </div>
        {other ? (
          <div>
            <div className="text-xs uppercase tracking-wide text-muted">{edge === "end" ? tr("Next") : tr("Previous")}</div>
            <div className="text-lg font-semibold">{label(other)}</div>
          </div>
        ) : (
          <div className="text-muted">{edge === "end" ? tr("There's no next chapter yet.") : tr("This is the first chapter.")}</div>
        )}
        {/* the buttons are buttons, not tap zones */}
        <div className="flex justify-center gap-2" onPointerDown={(e) => e.stopPropagation()} onPointerUp={(e) => e.stopPropagation()} onClick={(e) => e.stopPropagation()}>
          {edge === "end" && !other ? (
            <Link to={`/series/${chapter.seriesId}`} className={`${btn} bg-primary font-medium text-white`}>{t("Back to the series")}</Link>
          ) : (
            <button type="button" className={`${btn} border border-muted/40`} onClick={onBack}>{t("Back to the") + " "}{edge === "end" ? tr("last") : tr("first")}{" " + t("page")}</button>
          )}
          {other && (
            <button type="button" className={`${btn} bg-primary font-medium text-white`} onClick={onGo}>
              {edge === "end" ? tr("Next chapter") : tr("Previous chapter")}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
