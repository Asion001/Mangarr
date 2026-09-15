import { useQuery } from "@tanstack/react-query";
import { BookOpen } from "lucide-react";
import { Link } from "react-router";
import { api, apiUrl, unwrap } from "../../api/client";
import { Cover } from "../../components/Cover";
import { Badge, Progress } from "../../components/ui";
import { relative } from "../../lib/format";

/** ContinueReading is the shelf of series in progress, with the chapter to read next. */
export function ContinueReading() {
  const { data } = useQuery({
    queryKey: ["readers", "shelf"],
    queryFn: () => unwrap(api.GET("/api/v1/reading/shelf", { params: { query: { limit: 20 } } })),
    staleTime: 30_000,
  });
  if (!data || data.items.length === 0) return null;
  return (
    <section className="mb-6">
      <h2 className="mb-2 flex items-center gap-2 text-sm font-semibold">
        <BookOpen className="size-4 text-info" /> Continue reading
        <span className="font-normal text-muted">· {data.reader}</span>
      </h2>
      <div className="flex gap-3 overflow-x-auto pb-2">
        {data.items.map((it) => (
          <Link key={it.seriesId} to={`/series/${it.seriesId}`} className="group flex w-28 shrink-0 flex-col gap-1.5 sm:w-32" title={it.lastReadAt ? `last read ${relative(it.lastReadAt)}` : undefined}>
            <div className="relative">
              <Cover src={apiUrl(it.coverUrl)} alt={it.title} className="aspect-[2/3] w-full ring-accent/60 transition group-hover:ring-2" />
              {!it.next.available && (
                <div className="absolute bottom-1.5 left-1.5">
                  <Badge tone="warn" title="Reading apps stream it from the source and queue the download">
                    not downloaded
                  </Badge>
                </div>
              )}
            </div>
            <Progress value={it.total ? (100 * it.read) / it.total : 0} tone="ok" />
            <div className="line-clamp-2 text-xs font-medium leading-tight">{it.title}</div>
            <div className="-mt-0.5 text-xs text-muted">
              ch. {it.next.number} {it.page > 0 ? `· page ${it.page}` : "next"}
            </div>
          </Link>
        ))}
      </div>
    </section>
  );
}
