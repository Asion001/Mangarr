import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, FilePen, HardDrive, Pencil, RefreshCw, Search, Sparkles, Trash2, FileSearch, BookText } from "lucide-react";
import { api, apiUrl, unwrap } from "../../api/client";
import { usePushCommand, useSeries } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, Confirm, ErrorBox, Loading, Switch } from "../../components/ui";
import { bytes } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { statusTone } from "./SeriesIndex";
import { SourcesPanel } from "./SourcesPanel";
import { ChaptersTable } from "./ChaptersTable";
import { EditSeriesModal } from "./EditSeriesModal";
import { RenameModal } from "./Organize";

export function SeriesDetail() {
  const id = Number(useParams().id);
  const { data: s, isLoading, error } = useSeries(id);
  const push = usePushCommand();
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
            <Switch checked={s.monitored} onChange={setMonitored} label={s.monitored ? "Monitored" : "Unmonitored"} />
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
            <Stat label="Chapters" value={`${s.stats.fileCount} / ${s.stats.chapterCount}`} />
            <Stat label="Missing" value={String(s.stats.missingCount)} />
            <Stat label="Cleaned" value={String(s.stats.cleanedCount)} />
            <Stat label="On disk" value={s.stats.spaceSaved > 0 ? `${bytes(s.stats.sizeOnDisk)} (saved ${bytes(s.stats.spaceSaved)})` : bytes(s.stats.sizeOnDisk)} />
          </div>
          {(md.authors?.length || md.artists?.length) && (
            <p className="mt-3 text-sm text-muted">
              {md.authors?.length ? <>Story: {md.authors.join(", ")}</> : null}
              {md.artists?.length ? <> · Art: {md.artists.join(", ")}</> : null}
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
          <div className="mt-4 flex flex-wrap gap-2">
            <Button icon={<RefreshCw className="size-4" />} onClick={() => push.mutate({ name: "RefreshSeries", body: { seriesId: id }, label: "Refreshing sources" })}>
              Refresh
            </Button>
            <Button icon={<Search className="size-4" />} onClick={() => push.mutate({ name: "SearchMissing", body: { seriesId: id }, label: "Searching missing chapters" })}>
              Search missing
            </Button>
            <Button icon={<BookText className="size-4" />} onClick={() => push.mutate({ name: "RefreshMetadata", body: { seriesId: id }, label: "Refreshing metadata" })}>
              Metadata
            </Button>
            <Button icon={<Sparkles className="size-4" />} onClick={() => push.mutate({ name: "ProcessExisting", body: { seriesId: id }, label: "Downloaded chapters will be processed in the background" })}>
              Process existing
            </Button>
            <Button icon={<FilePen className="size-4" />} onClick={() => setRenaming(true)}>
              Rename files
            </Button>
            <Button icon={<FileSearch className="size-4" />} onClick={() => push.mutate({ name: "DiskScan", body: { seriesId: id }, label: "Scanning files" })}>
              Rescan disk
            </Button>
            <Button icon={<Pencil className="size-4" />} onClick={() => setEdit(true)}>
              Edit
            </Button>
            <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={() => setDel(true)}>
              Delete
            </Button>
          </div>
        </div>
      </div>

      <SourcesPanel series={s} />
      <ChaptersTable seriesId={id} />

      {edit && <EditSeriesModal series={s} onClose={() => setEdit(false)} />}
      {renaming && <RenameModal seriesIds={[id]} onClose={() => setRenaming(false)} />}
      <Confirm
        open={del}
        title="Delete series"
        danger
        confirmLabel="Delete"
        loading={deleting}
        message={
          <>
            Remove <b>{s.title}</b> from mangarr?
          </>
        }
        onConfirm={remove}
        onClose={() => setDel(false)}
      >
        <div className="mt-3">
          <Switch checked={deleteFiles} onChange={setDeleteFiles} label="Also move its folder to the recycle bin" />
        </div>
      </Confirm>
      <div className="mt-6 text-xs text-muted">
        <Link to="/activity/history" className="hover:text-fg">
          View history →
        </Link>
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
