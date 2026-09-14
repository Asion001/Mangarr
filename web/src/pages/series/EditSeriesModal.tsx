import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Lock, Unlock } from "lucide-react";
import { api, unwrap, type Series, type UpdateRequest } from "../../api/client";
import { useProfiles, useRootFolders, useTags } from "../../api/queries";
import { Button, Field, Input, Modal, Select, Switch, Textarea } from "../../components/ui";
import { useToast } from "../../lib/toast";
import { MetadataSearch } from "./AddSeries";

export function EditSeriesModal({ series, onClose }: { series: Series; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: profiles } = useProfiles();
  const { data: tags } = useTags();
  const [title, setTitle] = useState(series.title);
  const [monitored, setMonitored] = useState(series.monitored);
  const [monitorNew, setMonitorNew] = useState(series.monitorNew);
  const [profileId, setProfileId] = useState(series.profileId);
  const [direction, setDirection] = useState(series.readingDirection);
  const [language, setLanguage] = useState(series.language);
  const [status, setStatus] = useState(series.status);
  const [description, setDescription] = useState(series.metadata.description ?? "");
  const [tagIds, setTagIds] = useState<number[]>(series.tags ?? []);
  const [locks, setLocks] = useState<string[]>(series.metadata.locks ?? []);
  const [relink, setRelink] = useState(false);
  const { data: roots } = useRootFolders();
  const [rootId, setRootId] = useState(series.rootFolderId);
  const [folder, setFolder] = useState(series.path);
  const [moveFiles, setMoveFiles] = useState(true);
  const moving = rootId !== series.rootFolderId || folder.trim() !== series.path;
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    try {
      await unwrap(
        api.PUT("/api/v1/series/{id}", {
          params: { path: { id: series.id } },
          body: {
            title: title !== series.title ? title : undefined,
            monitored,
            monitorNew: monitorNew as UpdateRequest["monitorNew"],
            profileId,
            readingDirection: (direction !== series.readingDirection ? direction : undefined) as UpdateRequest["readingDirection"],
            language,
            status: (status !== series.status ? status : undefined) as UpdateRequest["status"],
            description: description !== (series.metadata.description ?? "") ? description : undefined,
            tags: tagIds,
            locks,
            rootFolderId: rootId !== series.rootFolderId ? rootId : undefined,
            path: folder.trim() !== series.path ? folder.trim() : undefined,
            moveFiles: moving ? moveFiles : undefined,
          },
        }),
      );
      qc.invalidateQueries({ queryKey: ["series"] });
      toast.success("Series saved", moving ? (moveFiles ? "Moving the files in the background" : "Location updated") : undefined);
      onClose();
    } catch (e) {
      toast.fromError(e);
    } finally {
      setSaving(false);
    }
  };

  if (relink) {
    return (
      <Modal open onClose={() => setRelink(false)} title="Fix metadata match" size="lg">
        <MetadataSearch
          initialQuery={series.title}
          onPick={async (c) => {
            try {
              await unwrap(api.PUT("/api/v1/series/{id}/metadata", { params: { path: { id: series.id } }, body: { moduleId: c.moduleId, provider: c.provider, id: c.id } }));
              qc.invalidateQueries({ queryKey: ["series"] });
              toast.success("Metadata updated");
              onClose();
            } catch (e) {
              toast.fromError(e);
            }
          }}
        />
      </Modal>
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`Edit ${series.title}`}
      size="lg"
      footer={
        <>
          <Button onClick={() => setRelink(true)} className="mr-auto">
            Fix metadata match…
          </Button>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={saving} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="Title" help="Editing locks the field against metadata refreshes." className="md:col-span-2">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Profile">
          <Select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
            {profiles?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="New chapters">
          <Select value={monitorNew} onChange={(e) => setMonitorNew(e.target.value)}>
            <option value="all">Monitor automatically</option>
            <option value="none">Don't monitor</option>
          </Select>
        </Field>
        <Field label="Reading direction" help="Written to ComicInfo (Manga field).">
          <Select value={direction} onChange={(e) => setDirection(e.target.value)}>
            <option value="rtl">Right to left (manga)</option>
            <option value="ltr">Left to right</option>
            <option value="vertical">Vertical</option>
            <option value="webtoon">Webtoon (long strip)</option>
          </Select>
        </Field>
        <Field label="Status">
          <Select value={status} onChange={(e) => setStatus(e.target.value)}>
            {["unknown", "ongoing", "completed", "hiatus", "cancelled"].map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Language (BCP-47)">
          <Input value={language} onChange={(e) => setLanguage(e.target.value)} placeholder="en" />
        </Field>
        <Field label="Tags">
          <div className="flex flex-wrap gap-2">
            {tags?.map((t) => (
              <label key={t.id} className="flex items-center gap-1 text-sm">
                <input type="checkbox" checked={tagIds.includes(t.id)} onChange={(e) => setTagIds(e.target.checked ? [...tagIds, t.id] : tagIds.filter((x) => x !== t.id))} />
                {t.label}
              </label>
            ))}
            {!tags?.length && <span className="text-xs text-muted">Create tags in Settings → General.</span>}
          </div>
        </Field>
        <Field label="Root folder">
          <Select value={rootId} onChange={(e) => setRootId(Number(e.target.value))}>
            {roots?.map((r) => (
              <option key={r.id} value={r.id}>
                {r.path}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Folder">
          <Input value={folder} onChange={(e) => setFolder(e.target.value)} />
        </Field>
        {moving && (
          <div className="md:col-span-2">
            <Switch checked={moveFiles} onChange={setMoveFiles} label="Move the files (off: they were already moved by hand)" />
            <p className="mt-1 text-xs text-muted">Reader progress on Komga/Kavita is restored after the move.</p>
          </div>
        )}
        <Field label="Description" className="md:col-span-2">
          <Textarea rows={5} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="md:col-span-2">
          <Switch checked={monitored} onChange={setMonitored} label="Monitored" />
        </div>
        {locks.length > 0 && (
          <Field label="Locked fields" help="Click to unlock; unlocked fields follow metadata refreshes again." className="md:col-span-2">
            <div className="flex flex-wrap gap-1.5">
              {locks.map((l) => (
                <button key={l} type="button" className="inline-flex items-center gap-1 rounded bg-panel-2 px-2 py-1 text-xs hover:bg-border" onClick={() => setLocks(locks.filter((x) => x !== l))}>
                  <Lock className="size-3" /> {l} <Unlock className="size-3 text-muted" />
                </button>
              ))}
            </div>
          </Field>
        )}
      </div>
    </Modal>
  );
}
