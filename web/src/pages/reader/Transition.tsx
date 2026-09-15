import type { S } from "../../api/client";

type Chapter = S["ReadChapter"];

const label = (c: { number: string; title?: string }) => `Ch. ${c.number}${c.title && c.title !== c.number && !c.title.endsWith(c.number) ? ` · ${c.title}` : ""}`;

/** Transition is the screen between chapters (Mihon's "Finished / Next"). */
export function Transition({ edge, chapter, onGo, onBack }: { edge: "start" | "end"; chapter: Chapter; onGo: () => void; onBack: () => void }) {
  const other = edge === "end" ? chapter.next : chapter.prev;
  return (
    <div className="flex h-full w-full items-center justify-center p-6" onPointerUp={(e) => e.stopPropagation()} onClick={(e) => e.stopPropagation()}>
      <div className="flex max-w-sm flex-col gap-4 text-center text-neutral-200">
        <div>
          <div className="text-xs uppercase tracking-wide text-neutral-400">{edge === "end" ? "Finished" : "Current"}</div>
          <div className="text-lg font-semibold">{label(chapter)}</div>
        </div>
        {other ? (
          <div>
            <div className="text-xs uppercase tracking-wide text-neutral-400">{edge === "end" ? "Next" : "Previous"}</div>
            <div className="text-lg font-semibold">{label(other)}</div>
          </div>
        ) : (
          <div className="text-neutral-400">{edge === "end" ? "There's no next chapter yet." : "This is the first chapter."}</div>
        )}
        <div className="flex justify-center gap-2">
          <button type="button" className="rounded-md border border-neutral-600 px-3 py-2 text-sm" onClick={onBack}>
            Back to the {edge === "end" ? "last" : "first"} page
          </button>
          {other && (
            <button type="button" className="rounded-md bg-orange-600 px-3 py-2 text-sm font-medium text-white" onClick={onGo}>
              {edge === "end" ? "Next chapter" : "Previous chapter"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
