import { t as tr, t } from "../../lib/i18n/core";
import { useQuery } from "@tanstack/react-query";
import { BookOpen } from "lucide-react";
import { Link } from "react-router";
import { api, apiUrl, unwrap } from "../../api/client";
import { Cover } from "../../components/Cover";
import { Badge, Progress } from "../../components/ui";
import { relative } from "../../lib/format";

/** useReadingShelf lists the series in progress, with the chapter to read next. */
export function useReadingShelf() {
  return useQuery({
    queryKey: ["readers", "shelf"],
    queryFn: () => unwrap(api.GET("/api/v1/reading/shelf", { params: { query: { limit: 20 } } })),
    staleTime: 30_000,
  });
}

/** ContinueReading is the shelf of series in progress, with the chapter to read next. */
export function ContinueReading() {
  const { data } = useReadingShelf();
  if (!data || data.items.length === 0) return null;
  return (
    <section className="mb-6">
      <h2 className="mb-2 flex items-center gap-2 text-sm font-semibold">
        <BookOpen className="size-4 text-info" />{" " + t("Continue reading")}<span className="font-normal text-muted">· {data.reader}</span>
      </h2>
      <div className="flex gap-3 overflow-x-auto pb-2">
        {data.items.map((it) => (
          <div key={it.seriesId} className="flex w-28 shrink-0 flex-col gap-1.5 sm:w-32" title={it.lastReadAt ? `last read ${relative(it.lastReadAt)}` : undefined}>
            <Link to={`/read/${it.next.chapterId}`} className="group relative" aria-label={`Read ${it.title} ch. ${it.next.number}`}>
              <Cover src={apiUrl(it.coverUrl)} alt={it.title} className="aspect-[2/3] w-full ring-accent/60 transition group-hover:ring-2" />
              <div className="absolute inset-0 flex items-center justify-center opacity-0 transition group-hover:opacity-100">
                <span className="rounded-full bg-black/70 p-2.5 text-white">
                  <BookOpen className="size-5" />
                </span>
              </div>
              {!it.next.available && (
                <div className="absolute bottom-1.5 left-1.5">
                  <Badge tone="warn" title={t("Streamed from the source; the download is queued when you open it")}>{t("not downloaded")}</Badge>
                </div>
              )}
            </Link>
            <Progress value={it.total ? (100 * it.read) / it.total : 0} tone="ok" />
            <Link to={`/series/${it.seriesId}`} className="line-clamp-2 text-xs font-medium leading-tight hover:text-accent-2">
              {it.title}
            </Link>
            <div className="-mt-0.5 text-xs text-muted">{t("ch.") + " "}{it.next.number} {it.page > 0 ? `· page ${it.page}` : tr("next")}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
