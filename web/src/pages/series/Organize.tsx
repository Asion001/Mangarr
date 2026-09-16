import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { useProfiles, useRootFolders, useTags } from "../../api/queries";
import {
  Button,
  ErrorBox,
  Field,
  Loading,
  Modal,
  Select,
  Switch,
} from "../../components/ui";
import { useToast } from "../../lib/toast";
import { BulkSourcesModal } from "./BulkSources";

/** RenameModal previews and applies renames to the current naming format. */
export function RenameModal({
  seriesIds,
  onClose,
}: {
  seriesIds: number[];
  onClose: () => void;
}) {
  const toast = useToast();
  const [folders, setFolders] = useState(false);
  const { data, isLoading, error } = useQuery({
    queryKey: ["rename-preview", seriesIds, folders],
    queryFn: () =>
      unwrap(
        api.POST("/api/v1/series/rename/preview", {
          body: { seriesIds, folders },
        }),
      ),
  });
  const [applying, setApplying] = useState(false);
  const files = (data ?? []).reduce((n, s) => n + s.files.length, 0);
  const dirs = (data ?? []).filter((s) => s.folderTo).length;
  const apply = async () => {
    setApplying(true);
    try {
      await unwrap(
        api.POST("/api/v1/series/rename", { body: { seriesIds, folders } }),
      );
      toast.success(
        "Renaming in the background",
        "Reader progress is restored on library servers afterwards.",
      );
      onClose();
    } catch (e) {
      toast.fromError(e);
    } finally {
      setApplying(false);
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title="Rename files"
      size="xl"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            loading={applying}
            disabled={!files && !dirs}
            onClick={apply}
          >
            Rename {files} files{dirs ? ` and ${dirs} folders` : ""}
          </Button>
        </>
      }
    >
      <Switch
        checked={folders}
        onChange={setFolders}
        label="Also rename series folders to the folder format"
      />
      <p className="mb-3 mt-2 text-xs text-muted">
        Names follow Settings → Media management. Library servers may treat
        renamed files as new books; mangarr restores readers' progress
        afterwards.
      </p>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && (
        <p className="text-sm text-muted">
          Everything already matches the naming format.
        </p>
      )}
      <div className="flex max-h-[55vh] flex-col gap-3 overflow-y-auto">
        {data?.map((s) => (
          <div key={s.seriesId}>
            <div className="text-sm font-medium">{s.title}</div>
            {s.folderTo && (
              <div className="font-mono text-xs">
                <span className="text-muted">{s.folderFrom}/</span> →{" "}
                <span className="text-accent-2">{s.folderTo}/</span>
              </div>
            )}
            {s.files.map((f) => (
              <div key={f.chapterId} className="font-mono text-xs break-all">
                <span className="text-muted">{f.from}</span> →{" "}
                <span className="text-accent-2">{f.to}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </Modal>
  );
}

type Editor = {
  monitored?: boolean;
  profileId?: number;
  rootFolderId?: number;
  moveFiles?: boolean;
  tags?: number[];
  tagMode?: "add" | "remove" | "replace";
};

/** MassEditBar edits the selected series. */
export function MassEditBar({
  ids,
  onClear,
}: {
  ids: number[];
  onClear: () => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: profiles } = useProfiles();
  const { data: roots } = useRootFolders();
  const { data: tags } = useTags();
  const [moving, setMoving] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [sourcing, setSourcing] = useState(false);
  const [rootId, setRootId] = useState(0);
  const [moveFiles, setMoveFiles] = useState(true);
  const edit = async (body: Editor, done?: string) => {
    try {
      const r = await unwrap(
        api.POST("/api/v1/series/editor", {
          body: { seriesIds: ids, ...body },
        }),
      );
      qc.invalidateQueries({ queryKey: ["series"] });
      toast.success(
        done ?? `${r.updated} series updated`,
        r.moves ? `${r.moves} moving in the background` : undefined,
      );
    } catch (e) {
      toast.fromError(e);
    }
  };
  // dialogs render outside the bar: its backdrop-blur would clip fixed children
  return (
    <>
      <div className="fixed inset-x-0 bottom-0 z-20 border-t border-border bg-panel/95 px-4 py-3 shadow-lg backdrop-blur md:left-56">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">{ids.length} selected</span>
          <Button size="sm" onClick={() => edit({ monitored: true })}>
            Monitor
          </Button>
          <Button size="sm" onClick={() => edit({ monitored: false })}>
            Unmonitor
          </Button>
          <Select
            className="w-40"
            value=""
            onChange={(e) =>
              e.target.value && edit({ profileId: Number(e.target.value) })
            }
          >
            <option value="">Set profile…</option>
            {profiles?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
          <Select
            className="w-40"
            value=""
            onChange={(e) => {
              const [mode, id] = e.target.value.split(":");
              if (id)
                edit({ tags: [Number(id)], tagMode: mode as "add" | "remove" });
            }}
          >
            <option value="">Tags…</option>
            {tags?.map((t) => (
              <option key={"a" + t.id} value={`add:${t.id}`}>
                + {t.label}
              </option>
            ))}
            {tags?.map((t) => (
              <option key={"r" + t.id} value={`remove:${t.id}`}>
                − {t.label}
              </option>
            ))}
          </Select>
          <Button size="sm" onClick={() => setMoving(true)}>
            Move…
          </Button>
          <Button size="sm" onClick={() => setRenaming(true)}>
            Rename files…
          </Button>
          <Button size="sm" onClick={() => setSourcing(true)}>
            Sources…
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto"
            onClick={onClear}
          >
            Clear selection
          </Button>
        </div>
      </div>
      {sourcing && <BulkSourcesModal ids={ids} onClose={() => setSourcing(false)} />}
      {moving && (
        <Modal
          open
          onClose={() => setMoving(false)}
          title={`Move ${ids.length} series`}
          footer={
            <>
              <Button onClick={() => setMoving(false)}>Cancel</Button>
              <Button
                variant="primary"
                disabled={!rootId}
                onClick={async () => {
                  await edit(
                    { rootFolderId: rootId, moveFiles },
                    "Moving in the background",
                  );
                  setMoving(false);
                }}
              >
                Move
              </Button>
            </>
          }
        >
          <Field label="Root folder">
            <Select
              value={rootId}
              onChange={(e) => setRootId(Number(e.target.value))}
            >
              <option value={0}>Choose…</option>
              {roots?.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.path}
                </option>
              ))}
            </Select>
          </Field>
          <div className="mt-3">
            <Switch
              checked={moveFiles}
              onChange={setMoveFiles}
              label="Move the files (off: they were already moved by hand)"
            />
          </div>
          <p className="mt-3 text-xs text-muted">
            Moving to a root folder in another Komga/Kavita library resets read
            progress there; mangarr writes readers' progress back once the
            server has scanned the new location (readers need linked accounts).
          </p>
        </Modal>
      )}
      {renaming && (
        <RenameModal seriesIds={ids} onClose={() => setRenaming(false)} />
      )}
    </>
  );
}
