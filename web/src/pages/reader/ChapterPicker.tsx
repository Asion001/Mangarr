import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Download, Search, X } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { Spinner } from "../../components/ui";
import { t } from "../../lib/i18n/core";

export function ChapterPicker({ seriesId, currentId, onPick, onClose }: { seriesId: number; currentId: number; onPick: (id: number) => void; onClose: () => void }) {
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  useEffect(() => {
    if (draft.trim() === query) return;
    const timeout = window.setTimeout(() => {
      setQuery(draft.trim());
      setPage(draft.trim() ? 1 : 0);
    }, 200);
    return () => window.clearTimeout(timeout);
  }, [draft, query]);
  const result = useQuery({
    queryKey: ["read-chapter-picker", seriesId, currentId, query, page],
    queryFn: () => unwrap(api.GET("/api/v1/read/series/{id}/chapters", { params: { path: { id: seriesId }, query: { q: query || undefined, currentId, page, pageSize: 50 } } })),
    placeholderData: (previous) => previous,
  });
  const data = result.data;
  const actualPage = data?.page ?? 1;
  const pages = Math.max(1, Math.ceil((data?.total ?? 0) / 50));

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-3 text-neutral-100" onPointerDown={(event) => event.target === event.currentTarget && onClose()}>
      <div className="flex max-h-[85dvh] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-neutral-700 bg-neutral-900 shadow-2xl">
        <div className="flex items-center gap-2 border-b border-neutral-700 p-3">
          <div className="relative min-w-0 flex-1">
            <Search className="absolute left-2.5 top-2.5 size-4 text-neutral-500" />
            <input autoFocus value={draft} onChange={(event) => setDraft(event.target.value)} placeholder={t("Find a chapter by number or title…")} className="w-full rounded-md border border-neutral-700 bg-neutral-950 py-2 pl-8 pr-3 text-sm outline-none focus:border-orange-500" />
          </div>
          <button type="button" className="rounded p-2 hover:bg-neutral-800" onClick={onClose} aria-label={t("Close chapter picker")}><X className="size-5" /></button>
        </div>
        <div className="min-h-40 flex-1 overflow-y-auto p-2">
          {result.isFetching && !data && <div className="flex justify-center p-8"><Spinner /></div>}
          {result.error && <p className="p-3 text-sm text-red-400">{String(result.error)}</p>}
          {data?.items.map((chapter) => (
            <button
              key={chapter.id}
              type="button"
              disabled={!chapter.available}
              onClick={() => onPick(chapter.id)}
              className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left ${chapter.id === currentId ? "bg-orange-500/20 text-orange-200" : "hover:bg-neutral-800"} disabled:cursor-not-allowed disabled:opacity-40`}
            >
              <span className="w-20 shrink-0 font-mono text-sm">{chapter.volume ? `v${chapter.volume} · ` : ""}{chapter.number}</span>
              <span className="min-w-0 flex-1 truncate text-sm">{chapter.title || `${t("Chapter")} ${chapter.number}`}</span>
              {chapter.downloaded && <Download className="size-3.5 text-neutral-400" aria-label={t("Downloaded")} />}
              {chapter.completed && <Check className="size-4 text-emerald-400" aria-label={t("Read")} />}
              {!chapter.available && <span className="text-xs text-neutral-500">{t("unavailable")}</span>}
            </button>
          ))}
          {data && data.items.length === 0 && <p className="p-6 text-center text-sm text-neutral-400">{t("No chapters match this search.")}</p>}
        </div>
        <div className="flex items-center justify-between border-t border-neutral-700 px-3 py-2 text-sm text-neutral-400">
          <button type="button" disabled={actualPage <= 1} onClick={() => setPage(actualPage - 1)} className="rounded px-3 py-1.5 hover:bg-neutral-800 disabled:opacity-30">{t("Previous")}</button>
          <span>{data?.total ?? 0} {t("chapters")} · {t("Page")} {actualPage} {t("of")} {pages}</span>
          <button type="button" disabled={actualPage >= pages} onClick={() => setPage(actualPage + 1)} className="rounded px-3 py-1.5 hover:bg-neutral-800 disabled:opacity-30">{t("Next")}</button>
        </div>
      </div>
    </div>
  );
}
