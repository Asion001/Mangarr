import { t as tr, t } from "../../lib/i18n/core";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Bell, BellOff, BookOpen, ExternalLink, Eye, FilePen, HardDrive, Pencil, RefreshCw, Search, Sparkles, Trash2, FileSearch, BookText } from "lucide-react";
import { api, apiUrl, unwrap, type Chapter, type S } from "../../api/client";
import { useChapters, usePushCommand, useSeries } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, Confirm, ErrorBox, Loading, Switch } from "../../components/ui";
import { bytes, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { statusTone } from "./SeriesIndex";
import { SourcesPanel } from "./SourcesPanel";
import { ChaptersTable, readable } from "./ChaptersTable";
import { EditSeriesModal } from "./EditSeriesModal";
import { RenameModal } from "./Organize";
import { useAccount } from "../../lib/account";

export function SeriesDetail() {
  const id = Number(useParams().id);
  const { data: s, isLoading, error } = useSeries(id);
  const { data: chapters } = useChapters(id);
  const push = usePushCommand();
  const { can, account } = useAccount();
  const manage = can("library.manage");
  const qc = useQueryClient();
  const toast = useToast();
  const nav = useNavigate();
  const [edit, setEdit] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [del, setDel] = useState(false);
  const [deleteFiles, setDeleteFiles] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [showDesc, setShowDesc] = useState(false);

  if (isLoading) return <Loading />;
  if (error || !s) return <ErrorBox error={error ?? "Series not found"} />;
  const readTarget = readTargetOf(chapters, s.reading?.nextUnread);

  const setMonitored = async (v: boolean) => {
    try {
      await unwrap(api.PUT("/api/v1/series/{id}", { params: { path: { id } }, body: { monitored: v } }));
      qc.invalidateQueries({ queryKey: ["series"] });
    } catch (e) {
      toast.fromError(e);
    }
  };

  const remove = async () => {
    setDeleting(true);
    try {
      await unwrap(api.DELETE("/api/v1/series/{id}", { params: { path: { id }, query: { deleteFiles } } }));
      toast.success(`${s.title} deleted`);
      qc.invalidateQueries({ queryKey: ["series"] });
      nav("/");
    } catch (e) {
      toast.fromError(e);
    } finally {
      setDeleting(false);
    }
  };

  const md = s.metadata;
  const links = Object.entries(md.links ?? {});
  return (
    <>
      <div className="mb-6 flex flex-col gap-5 md:flex-row">
        <Cover src={apiUrl(s.coverUrl)} alt={s.title} className="aspect-[2/3] w-40 shrink-0 self-start md:w-48" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <h1 className="text-2xl font-semibold leading-tight">{s.title}</h1>
              {md.altTitles && md.altTitles.length > 0 && <p className="mt-1 line-clamp-1 text-sm text-muted">{md.altTitles.slice(0, 4).join(" · ")}</p>}
            </div>
            <div className="flex items-center gap-3">
              {account?.kind === "user" && <FollowButton seriesId={id} following={s.following} />}
              {manage && <Switch checked={s.monitored} onChange={setMonitored} label={s.monitored ? tr("Monitored") : tr("Unmonitored")} />}
            </div>
          </div>
          <div className="mt-3 flex flex-wrap gap-1.5">
            <Badge tone={statusTone(s.status)}>{s.status}</Badge>
            {md.format && <Badge>{md.format}</Badge>}
            {md.year ? <Badge>{md.year}</Badge> : null}
            <Badge>{s.readingDirection}</Badge>
            {s.language && <Badge>{s.language}</Badge>}
            {md.ageRating && <Badge tone="warn">{md.ageRating}</Badge>}
            {(md.genres ?? []).slice(0, 8).map((g) => (
              <Badge key={g} tone="info">
                {g}
              </Badge>
            ))}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-sm sm:grid-cols-4">
            <Stat label={t("Chapters")} value={`${s.stats.fileCount} / ${s.stats.chapterCount}`} />
            <Stat label={t("Missing")} value={String(s.stats.missingCount)} />
            <Stat label={t("Cleaned")} value={String(s.stats.cleanedCount)} />
            <Stat label={t("On disk")} value={s.stats.spaceSaved > 0 ? `${bytes(s.stats.sizeOnDisk)} (saved ${bytes(s.stats.spaceSaved)})` : bytes(s.stats.sizeOnDisk)} />
          </div>
          {readTarget && (
            <Link to={`/read/${readTarget.id}`} className="mt-3 inline-flex items-center gap-2 rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white hover:bg-accent-2">
              <BookOpen className="size-4" /> {readTarget.label}
            </Link>
          )}
          {s.reading && (s.reading.readers.length > 0 || s.reading.webUrl) && (
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              {s.reading.nextUnread && !readTarget && (
                <span className="flex items-center gap-1">
                  <BookOpen className="size-4 text-info" />{t("Continue: ch.") + " "}{s.reading.nextUnread.number}
                  {s.reading.nextUnread.title && s.reading.nextUnread.title !== s.reading.nextUnread.number && (
                    <span className="text-muted">{s.reading.nextUnread.title}</span>
                  )}
                  {!s.reading.nextUnread.available && <Badge tone="warn">{t("not downloaded")}</Badge>}
                </span>
              )}
              {s.reading.readers.map((r) => (
                <span key={r.readerId} className="text-muted" title={r.lastReadAt ? `last read ${relative(r.lastReadAt)}` : undefined}>
                  <Eye className="mr-1 inline size-3.5" />
                  {r.reader}: {r.read}/{s.stats.chapterCount}{" " + t("read")}{r.inProgress > 0 && `, ${r.inProgress} started`}
                </span>
              ))}
              {s.reading.webUrl && (
                <a href={s.reading.webUrl} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-accent-2 hover:underline">
                  <ExternalLink className="size-3.5" />{" " + t("Open in") + " "}{s.reading.webName || tr("library")}
                </a>
              )}
            </div>
          )}
          {(md.authors?.length || md.artists?.length) && (
            <p className="mt-3 text-sm text-muted">
              {md.authors?.length ? <>{t("Story:") + " "}{md.authors.join(", ")}</> : null}
              {md.artists?.length ? <>{" " + t("· Art:") + " "}{md.artists.join(", ")}</> : null}
            </p>
          )}
          {md.description && (
            <p className={`mt-3 whitespace-pre-line text-sm text-fg/85 ${showDesc ? "" : "line-clamp-3"} cursor-pointer`} onClick={() => setShowDesc(!showDesc)}>
              {md.description}
            </p>
          )}
          <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-muted">
            <span className="flex items-center gap-1">
              <HardDrive className="size-3.5" /> {s.fullPath}
            </span>
            {links.map(([k, v]) => (
              <a key={k} href={v} target="_blank" rel="noreferrer" className="flex items-center gap-1 hover:text-accent-2">
                <ExternalLink className="size-3.5" /> {k}
              </a>
            ))}
          </div>
          {manage && <div className="mt-4 flex flex-wrap gap-2">
            <Button icon={<RefreshCw className="size-4" />} onClick={() => push.mutate({ name: "RefreshSeries", body: { seriesId: id }, label: "Refreshing sources" })}>{t("Refresh")}</Button>
            <Button icon={<Search className="size-4" />} onClick={() => push.mutate({ name: "SearchMissing", body: { seriesId: id }, label: "Searching missing chapters" })}>{t("Search missing")}</Button>
            <Button icon={<BookText className="size-4" />} onClick={() => push.mutate({ name: "RefreshMetadata", body: { seriesId: id }, label: "Refreshing metadata" })}>{t("Metadata")}</Button>
            <Button icon={<Sparkles className="size-4" />} onClick={() => push.mutate({ name: "ProcessExisting", body: { seriesId: id }, label: "Downloaded chapters will be processed in the background" })}>{t("Process existing")}</Button>
            <Button icon={<FilePen className="size-4" />} onClick={() => setRenaming(true)}>{t("Rename files")}</Button>
            <Button icon={<FileSearch className="size-4" />} onClick={() => push.mutate({ name: "DiskScan", body: { seriesId: id }, label: "Scanning files" })}>{t("Rescan disk")}</Button>
            <Button icon={<Pencil className="size-4" />} onClick={() => setEdit(true)}>{t("Edit")}</Button>
            <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={() => setDel(true)}>{t("Delete")}</Button>
          </div>}
        </div>
      </div>

      {manage && <SourcesPanel series={s} />}
      <ChaptersTable seriesId={id} manage={manage} />

      {edit && <EditSeriesModal series={s} onClose={() => setEdit(false)} />}
      {renaming && <RenameModal seriesIds={[id]} onClose={() => setRenaming(false)} />}
      <Confirm
        open={del}
        title={t("Delete series")}
        danger
        confirmLabel={t("Delete")}
        loading={deleting}
        message={
          <>{t("Remove") + " "}<b>{s.title}</b>{" " + t("from mangarr?")}</>
        }
        onConfirm={remove}
        onClose={() => setDel(false)}
      >
        <div className="mt-3">
          <Switch checked={deleteFiles} onChange={setDeleteFiles} label={t("Also move its folder to the recycle bin")} />
        </div>
      </Confirm>
      <div className="mt-6 text-xs text-muted">
        <Link to="/activity/history" className="hover:text-fg">{t("View history →")}</Link>
      </div>
    </>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-muted">{label}</div>
      <div className="font-medium">{value}</div>
    </div>
  );
}

/** readTargetOf picks what the Read button opens: where you left off, or
 * the first chapter when you haven't started. Nothing when that chapter
 * can't be read (not downloaded and no source). */
function readTargetOf(chapters: Chapter[] | undefined, next?: S["NextChapter"]) {
  if (!chapters) return null;
  if (next) {
    const c = chapters.find((c) => c.id === next.chapterId);
    return c && readable(c) ? { id: c.id, label: `Continue ch. ${c.number}` } : null;
  }
  const first = chapters.filter(readable).sort((a, b) => a.numberSort - b.numberSort)[0];
  return first ? { id: first.id, label: `Start reading ch. ${first.number}` } : null;
}

/** FollowButton: follow a series to get its new chapters on your notification targets. */
function FollowButton({ seriesId, following }: { seriesId: number; following: boolean }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const toggle = async () => {
    setBusy(true);
    try {
      const path = { params: { path: { id: seriesId } } };
      await unwrap(following ? api.DELETE("/api/v1/series/{id}/follow", path) : api.PUT("/api/v1/series/{id}/follow", path));
      qc.invalidateQueries({ queryKey: ["series"] });
    } catch (e) {
      toast.fromError(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button
      size="sm"
      variant={following ? "secondary" : undefined}
      loading={busy}
      icon={following ? <BellOff className="size-4" /> : <Bell className="size-4" />}
      onClick={toggle}
      title={following ? tr("Stop getting its new chapters") : tr("Get its new chapters on your notifications (set them up under My account)")}
    >
      {following ? tr("Following") : tr("Follow")}
    </Button>
  );
}
