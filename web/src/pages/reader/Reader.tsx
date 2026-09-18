import { t as tr, t } from "../../lib/i18n/core";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, ChevronLeft, ChevronRight, Download, Maximize, Minimize, Settings2, X } from "lucide-react";
import clsx from "clsx";
import { api, apiUrl, basePath, unwrap } from "../../api/client";
import { ErrorBox, Spinner } from "../../components/ui";
import { useToast } from "../../lib/toast";
import { useReaderSettings } from "./settings";
import { displayWidth, pageUrl, useDims, useViewport, type Half } from "./page";
import { buildViews, indexOf, PagedViewer } from "./PagedViewer";
import { WebtoonViewer } from "./WebtoonViewer";
import { SettingsPanel } from "./SettingsPanel";

const chapterQuery = (id: number) => ({
  queryKey: ["read-chapter", id],
  queryFn: () => unwrap(api.GET("/api/v1/read/chapters/{id}", { params: { path: { id } } })),
  staleTime: 60_000,
});

/** ReaderPage is the full-screen web reader (/read/:id). */
export function ReaderPage() {
  const { id } = useParams();
  return <Reader key={id} chapterId={Number(id)} />;
}

type Pos = { page: number; half?: Half; edge?: "start" | "end" };

function Reader({ chapterId }: { chapterId: number }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const toast = useToast();
  const [search] = useSearchParams();
  const { data: ch, error } = useQuery(chapterQuery(chapterId));
  const { settings: s, set, saveAsDefault, reset, hasOwn, loading: settingsLoading } = useReaderSettings(ch?.seriesId ?? 0, ch?.readingDirection ?? "");
  const { dims, need, natural } = useDims(chapterId, s.crop || s.splitWide || (s.mode === "paged" && s.spread !== "single"));
  const vp = useViewport();
  const views = useMemo(() => (ch ? buildViews(ch.pages.length, dims, s, vp.w > vp.h) : []), [ch, dims, s, vp.w, vp.h]);
  const [pos, setPos] = useState<Pos | null>(null);
  const [bars, setBars] = useState(true);
  const [panel, setPanel] = useState(false);
  const [full, setFull] = useState(!!document.fullscreenElement);
  const [marking, setMarking] = useState("");

  // where to start: ?page=, else where you left off
  useEffect(() => {
    if (!ch || pos) return;
    const n = ch.pages.length;
    const q = search.get("page");
    let p = 1;
    if (q === "last") p = n;
    else if (q && Number(q) > 0) p = Math.min(Number(q), n);
    else if (ch.progress.page > 0 && !ch.progress.completed) p = Math.min(ch.progress.page, n);
    setPos({ page: Math.max(p, 1) });
  }, [ch, pos, search]);

  const index = !pos ? 1 : pos.edge ? (pos.edge === "start" ? 0 : views.length - 1) : indexOf(views, pos.page, pos.half);
  const view = views[index];
  const setIndex = useCallback(
    (i: number) => {
      const v = views[i];
      if (!v) return;
      if ("edge" in v) setPos((p) => ({ page: p?.page ?? 1, edge: v.edge }));
      else setPos({ page: v.pages[0], half: v.half });
    },
    [views],
  );
  const [webPage, setWebPage] = useState(0);
  const count = ch?.pages.length ?? 0;
  // the page you're on (for progress): the last one on screen
  const page = !ch
    ? 0
    : s.mode === "webtoon"
      ? webPage || pos?.page || 1
      : view && "pages" in view
        ? Math.max(...view.pages)
        : pos?.edge === "end"
          ? count
          : pos?.page ?? 1;

  // bounds for the pages around the current one (paged mode)
  useEffect(() => {
    if (!ch || s.mode !== "paged") return;
    const around: number[] = [];
    for (let k = Math.max(1, page - 2); k <= Math.min(count, page + s.preload + 1); k++) around.push(k);
    need(around);
  }, [ch, page, count, s.mode, s.preload, need]);

  // preload the next pages, and near the end the next chapter
  useEffect(() => {
    if (!ch || s.mode !== "paged") return;
    const w = displayWidth(vp.w);
    for (let k = page + 1; k <= Math.min(count, page + s.preload); k++) new Image().src = pageUrl(ch.id, k, w);
    if (ch.next && page >= count - 2) {
      const next = ch.next.id;
      void qc.prefetchQuery(chapterQuery(next));
      for (const k of [1, 2]) new Image().src = pageUrl(next, k, w);
    }
  }, [ch, page, count, s.mode, s.preload, vp.w, qc]);

  // save progress (debounced; right away when leaving)
  const saved = useRef(0);
  const pending = useRef(0);
  const flush = useCallback(() => {
    const p = pending.current;
    if (!ch || !p || p === saved.current) return;
    saved.current = p;
    void fetch(`${basePath}/api/v1/read/chapters/${ch.id}/progress`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ page: p }),
      keepalive: true,
    }).then(() => {
      qc.invalidateQueries({ queryKey: ["readers"] });
      qc.invalidateQueries({ queryKey: ["series"] });
    });
  }, [ch, qc]);
  useEffect(() => {
    if (!ch || !page || !pos) return;
    pending.current = page;
    if (page === count) return flush(); // finished
    const t = window.setTimeout(flush, 1500);
    return () => window.clearTimeout(t);
  }, [ch, page, count, pos, flush]);
  useEffect(() => {
    const hide = () => document.visibilityState === "hidden" && flush();
    document.addEventListener("visibilitychange", hide);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      flush();
    };
  }, [flush]);

  // keep the screen on
  useEffect(() => {
    if (!s.keepAwake || !("wakeLock" in navigator)) return;
    let lock: WakeLockSentinel | undefined;
    const get = () => {
      if (document.visibilityState === "visible") navigator.wakeLock.request("screen").then((l) => (lock = l), () => undefined);
    };
    get();
    document.addEventListener("visibilitychange", get);
    return () => {
      document.removeEventListener("visibilitychange", get);
      void lock?.release();
    };
  }, [s.keepAwake]);

  // bars hide on their own after opening
  useEffect(() => {
    const t = window.setTimeout(() => setBars(false), 2500);
    return () => window.clearTimeout(t);
  }, [chapterId]);

  useEffect(() => {
    if (ch) document.title = `${ch.seriesTitle} ch. ${ch.number} · mangarr`;
    return () => {
      document.title = "mangarr";
    };
  }, [ch]);

  const mark = async (read: boolean, scope: "chapter" | "previous") => {
    if (!ch) return;
    const key = `${scope}:${read}`;
    setMarking(key);
    try {
      await unwrap(api.PUT("/api/v1/read/chapters/{id}/mark", { params: { path: { id: ch.id } }, body: { read, scope } }));
      if (scope === "chapter") {
        saved.current = pending.current;
        qc.setQueryData(chapterQuery(ch.id).queryKey, { ...ch, progress: { page: read ? count : 0, completed: read } });
      }
      qc.invalidateQueries({ queryKey: ["series"] });
      qc.invalidateQueries({ queryKey: ["readers"] });
    } catch (e) {
      toast.fromError(e, tr("Could not update read status"));
    } finally {
      setMarking("");
    }
  };

  const toggleFull = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void document.documentElement.requestFullscreen?.();
  };
  useEffect(() => {
    const on = () => setFull(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", on);
    return () => document.removeEventListener("fullscreenchange", on);
  }, []);

  const goChapter = useCallback(
    (dir: "prev" | "next") => {
      const target = dir === "next" ? ch?.next : ch?.prev;
      if (!ch) return;
      if (!target) {
        if (dir === "next") setPos({ page: count, edge: "end" });
        return;
      }
      flush();
      navigate(`/read/${target.id}${dir === "prev" ? "?page=last" : ""}`, { replace: true });
    },
    [ch, count, flush, navigate],
  );

  // global keys
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
      if (e.key === "Escape") {
        if (panel) setPanel(false);
        else if (ch) navigate(`/series/${ch.seriesId}`);
      } else if (e.key === "m") setBars((b) => !b);
      else if (e.key === "f") toggleFull();
      else if (e.key === "n" && ch?.next) goChapter("next");
      else if (e.key === "p" && ch?.prev) goChapter("prev");
    };
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, [panel, ch, navigate, goChapter]);

  const bg = s.background === "white" ? "bg-white" : s.background === "gray" ? "bg-zinc-700" : "bg-black";
  if (error) {
    return (
      <div className="flex h-dvh flex-col items-center justify-center gap-4 bg-black p-6 text-neutral-200">
        <ErrorBox error={error} />
        <button type="button" className="text-sm text-orange-400 hover:underline" onClick={() => navigate(-1)}>{t("Go back")}</button>
      </div>
    );
  }
  if (!ch || !pos || settingsLoading) {
    return (
      <div className="flex h-dvh items-center justify-center bg-black text-neutral-400">
        <Spinner />
      </div>
    );
  }
  const pageViews = views.filter((v) => "pages" in v).length;
  const sliderIndex = Math.min(Math.max(index, 1), pageViews);

  return (
    <div className={clsx("fixed inset-0 select-none overflow-hidden", bg)}>
      {s.mode === "webtoon" ? (
        <WebtoonViewer
          chapter={ch}
          settings={s}
          startPage={pos.page}
          onPage={setWebPage}
          dims={dims}
          need={need}
          natural={natural}
          onChapter={goChapter}
          onMenu={() => setBars((b) => !b)}
        />
      ) : (
        <PagedViewer
          chapter={ch}
          settings={s}
          views={views}
          index={index}
          setIndex={setIndex}
          dims={dims}
          natural={natural}
          onChapter={goChapter}
          onMenu={() => setBars((b) => !b)}
        />
      )}

      {/* top bar */}
      <div
        className={clsx(
          "absolute inset-x-0 top-0 z-20 flex items-center gap-2 bg-neutral-950/90 px-2 py-2 text-neutral-100 backdrop-blur transition-transform",
          bars ? "translate-y-0" : "-translate-y-full",
        )}
        style={{ paddingTop: "max(0.5rem, env(safe-area-inset-top))" }}
      >
        <Link to={`/series/${ch.seriesId}`} className="rounded p-2 hover:bg-neutral-800" aria-label={t("Back to the series")}>
          <ArrowLeft className="size-5" />
        </Link>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{ch.seriesTitle}</div>
          <div className="truncate text-xs text-neutral-400">{t("Ch.") + " "}{ch.number}
            {ch.title && ch.title !== ch.number && !ch.title.endsWith(ch.number) ? ` · ${ch.title}` : ""}
            {!ch.downloaded && tr(" · streamed")}
          </div>
        </div>
        <button
          type="button"
          disabled={!!marking}
          className="flex items-center gap-1 rounded px-2 py-2 text-xs hover:bg-neutral-800 disabled:opacity-40"
          onClick={() => void mark(!ch.progress.completed, "chapter")}
          title={ch.progress.completed ? tr("Mark chapter unread") : tr("Mark chapter read")}
        >
          {ch.progress.completed ? <X className="size-5" /> : <Check className="size-5" />}
          <span className="hidden xl:inline">{ch.progress.completed ? t("Mark unread") : t("Mark read")}</span>
        </button>
        {ch.prev && (
          <>
            <button
              type="button"
              disabled={!!marking}
              className="rounded p-2 text-neutral-300 hover:bg-neutral-800 disabled:opacity-40"
              onClick={() => void mark(true, "previous")}
              title={t("Mark previous chapters read")}
              aria-label={t("Mark previous chapters read")}
            >
              <span className="flex items-center text-xs"><Check className="size-5" /><ChevronLeft className="-ml-1 size-3" /></span>
            </button>
            <button
              type="button"
              disabled={!!marking}
              className="rounded p-2 text-neutral-300 hover:bg-neutral-800 disabled:opacity-40"
              onClick={() => void mark(false, "previous")}
              title={t("Mark previous chapters unread")}
              aria-label={t("Mark previous chapters unread")}
            >
              <span className="flex items-center text-xs"><X className="size-5" /><ChevronLeft className="-ml-1 size-3" /></span>
            </button>
          </>
        )}
        {ch.canDownload && (
          <a href={apiUrl(`api/v1/read/chapters/${ch.id}/file`)} download className="rounded p-2 hover:bg-neutral-800" aria-label={t("Download the chapter")}>
            <Download className="size-5" />
          </a>
        )}
        <button type="button" className="rounded p-2 hover:bg-neutral-800" onClick={toggleFull} aria-label={t("Full screen")}>
          {full ? <Minimize className="size-5" /> : <Maximize className="size-5" />}
        </button>
        <button type="button" className="rounded p-2 hover:bg-neutral-800" onClick={() => setPanel((p) => !p)} aria-label={t("Reader settings")}>
          <Settings2 className="size-5" />
        </button>
      </div>

      {/* bottom bar */}
      <div
        className={clsx(
          "absolute inset-x-0 bottom-0 z-20 flex items-center gap-2 bg-neutral-950/90 px-2 py-2 text-neutral-100 backdrop-blur transition-transform",
          bars ? "translate-y-0" : "translate-y-full",
        )}
        style={{ paddingBottom: "max(0.5rem, env(safe-area-inset-bottom))" }}
      >
        <button
          type="button"
          className="rounded p-2 hover:bg-neutral-800 disabled:opacity-30"
          disabled={!ch.prev}
          onClick={() => goChapter("prev")}
          aria-label={t("Previous chapter")}
        >
          <ChevronLeft className="size-5" />
        </button>
        {s.mode === "paged" ? (
          <input
            type="range"
            min={1}
            max={Math.max(pageViews, 1)}
            value={sliderIndex}
            onChange={(e) => setIndex(Number(e.target.value))}
            className="flex-1 accent-orange-500"
            style={{ direction: s.direction === "rtl" ? "rtl" : "ltr" }}
            aria-label={t("Page")}
          />
        ) : (
          <div className="flex-1" />
        )}
        <span className="w-16 text-center text-xs tabular-nums text-neutral-300">
          {page} / {count}
        </span>
        <button
          type="button"
          className="rounded p-2 hover:bg-neutral-800 disabled:opacity-30"
          disabled={!ch.next}
          onClick={() => goChapter("next")}
          aria-label={t("Next chapter")}
        >
          <ChevronRight className="size-5" />
        </button>
      </div>

      {!bars && s.showPageNumber && (
        <div className="pointer-events-none absolute bottom-2 left-1/2 z-10 -translate-x-1/2 rounded-full bg-black/60 px-2.5 py-0.5 text-xs tabular-nums text-neutral-200" style={{ marginBottom: "env(safe-area-inset-bottom)" }}>
          {page} / {count}
        </div>
      )}

      {panel && <SettingsPanel s={s} set={set} onClose={() => setPanel(false)} hasOwn={hasOwn} saveAsDefault={saveAsDefault} reset={reset} />}
    </div>
  );
}
