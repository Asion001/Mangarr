import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderPlus, Trash2 } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { useRootFolders } from "../../api/queries";
import { Badge, Button, Card, ErrorBox, Field, IconButton, Input, Loading, PageHeader, Switch, Table, Td, Th } from "../../components/ui";
import { bytes } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useSettingsDoc } from "./useSettingsDoc";

type Media = S["MediaManagement"];

export function MediaPage() {
  const { value: m, patch, save, saving, isLoading, error } = useSettingsDoc<Media>("media");
  const { data: preview } = useQuery({
    queryKey: ["naming-preview", m?.chapterFormat, m?.seriesFolderFormat],
    queryFn: () => unwrap(api.GET("/api/v1/settings/media/preview", { params: { query: { chapterFormat: m!.chapterFormat, folderFormat: m!.seriesFolderFormat } } })),
    enabled: !!m,
  });
  return (
    <>
      <PageHeader
        title="Media management"
        actions={
          <Button variant="primary" loading={saving} onClick={() => save()}>
            Save
          </Button>
        }
      />
      <RootFolders />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {m && (
        <>
          <Card title="File naming" className="mb-6">
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Chapter file format" help="Tokens: {Series Title} {Series CleanTitle} {Series Year} {Chapter:0000} {Volume:00} {Chapter Title} {Scanlator} {Source} {Language}. [ ] = optional group.">
                <Input value={m.chapterFormat} onChange={(e) => patch({ chapterFormat: e.target.value })} />
              </Field>
              <Field label="Series folder format">
                <Input value={m.seriesFolderFormat} onChange={(e) => patch({ seriesFolderFormat: e.target.value })} />
              </Field>
            </div>
            {preview && (
              <div className="mt-4 rounded-md bg-bg p-3 font-mono text-xs text-muted">
                <div>{preview.folder}/</div>
                <div className="ml-4">{preview.chapter}</div>
                <div className="ml-4">{preview.decimal}</div>
                <div className="ml-4">{preview.volume}</div>
              </div>
            )}
            <p className="mt-3 text-xs text-warn">
              Keep scanlator, source and volume out of file names: upgrades replace files in place, and changing names later breaks read progress in Komga/Kavita.
            </p>
          </Card>
          <Card title="Library files" className="mb-6">
            <div className="grid gap-4 md:grid-cols-2">
              <Switch checked={m.writeSeriesJson} onChange={(v) => patch({ writeSeriesJson: v })} label="Write series.json (Komga series metadata)" />
              <Switch checked={m.writeCover} onChange={(v) => patch({ writeCover: v })} label="Write cover.jpg" />
              <Switch checked={m.writeVolume} onChange={(v) => patch({ writeVolume: v })} label="Write volume numbers into ComicInfo.xml" />
              <Field label="Minimum free space (MB)" help="Downloads pause when a root folder has less.">
                <Input type="number" value={m.minFreeSpaceMb} onChange={(e) => patch({ minFreeSpaceMb: Number(e.target.value) })} />
              </Field>
              <Field label="File permissions" help="Octal, e.g. 0664">
                <Input value={m.fileMode} onChange={(e) => patch({ fileMode: e.target.value })} />
              </Field>
              <Field label="Folder permissions">
                <Input value={m.dirMode} onChange={(e) => patch({ dirMode: e.target.value })} />
              </Field>
            </div>
          </Card>
          <Card title="Recycle bin">
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Recycle bin folder" help="Replaced and cleaned files go here. Empty = inside the data folder.">
                <Input value={m.recycleBinPath} onChange={(e) => patch({ recycleBinPath: e.target.value })} />
              </Field>
              <Field label="Keep recycled files (days)" help="0 = forever">
                <Input type="number" value={m.recycleBinDays} onChange={(e) => patch({ recycleBinDays: Number(e.target.value) })} />
              </Field>
            </div>
          </Card>
        </>
      )}
    </>
  );
}

function RootFolders() {
  const { data, isLoading } = useRootFolders();
  const qc = useQueryClient();
  const toast = useToast();
  const [path, setPath] = useState("");
  const [lang, setLang] = useState("en");
  const add = async () => {
    try {
      await unwrap(api.POST("/api/v1/rootfolders", { body: { path, language: lang } }));
      setPath("");
      qc.invalidateQueries({ queryKey: ["rootfolders"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const remove = async (id: number) => {
    try {
      await unwrap(api.DELETE("/api/v1/rootfolders/{id}", { params: { path: { id } } }));
      qc.invalidateQueries({ queryKey: ["rootfolders"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Card title="Root folders" className="mb-6">
      {isLoading && <Loading />}
      {data && data.length > 0 && (
        <Table className="mb-4">
          <thead>
            <tr>
              <Th>Path</Th>
              <Th>Language</Th>
              <Th>Series</Th>
              <Th>Free space</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((r) => (
              <tr key={r.id}>
                <Td className="font-mono text-xs">{r.path}</Td>
                <Td>{r.language || "—"}</Td>
                <Td>{r.seriesCount}</Td>
                <Td>{r.accessible ? bytes(r.freeSpace) : <Badge tone="err">{r.error}</Badge>}</Td>
                <Td className="text-right">
                  <IconButton title="Remove" onClick={() => remove(r.id)} disabled={r.seriesCount > 0}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <div className="flex flex-wrap gap-2">
        <Input className="max-w-md flex-1" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/data/manga/en" />
        <Input className="w-24" value={lang} onChange={(e) => setLang(e.target.value)} placeholder="en" />
        <Button icon={<FolderPlus className="size-4" />} disabled={!path} onClick={add}>
          Add root folder
        </Button>
      </div>
      <p className="mt-2 text-xs text-muted">Use one root folder per language. Mount the same folder read-only into Komga/Kavita.</p>
    </Card>
  );
}
