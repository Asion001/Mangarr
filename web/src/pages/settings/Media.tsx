import { t as tr, t } from "../../lib/i18n/core";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderInput, FolderPlus, Trash2 } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { useRootFolders } from "../../api/queries";
import { Badge, Button, Card, EnvLock, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, SaveBar, Switch, Table, Td, Th } from "../../components/ui";
import { bytes } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useSettingsDoc } from "./useSettingsDoc";

type Media = S["MediaManagement"];

export function MediaPage() {
  const { value: m, patch, save, saving, isLoading, error, lock, dirty, reset } = useSettingsDoc<Media>("media");
  const { data: preview } = useQuery({
    queryKey: ["naming-preview", m?.chapterFormat, m?.seriesFolderFormat],
    queryFn: () => unwrap(api.GET("/api/v1/settings/media/preview", { params: { query: { chapterFormat: m!.chapterFormat, folderFormat: m!.seriesFolderFormat } } })),
    enabled: !!m,
  });
  return (
    <>
      <PageHeader
        title={t("Media management")}
      />
      <RootFolders />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {m && (
        <>
          <Card title={t("File naming")} className="mb-6">
            <div className="grid gap-4 md:grid-cols-2">
              <Field env={lock("chapterFormat")} label={t("Chapter file format")} help={t("Tokens: {Series Title} {Series CleanTitle} {Series Year} {Chapter:0000} {Volume:00} {Chapter Title} {Scanlator} {Source} {Language}. [ ] = optional group.")}>
                <Input value={m.chapterFormat} onChange={(e) => patch({ chapterFormat: e.target.value })} />
              </Field>
              <Field env={lock("seriesFolderFormat")} label={t("Series folder format")}>
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
            <p className="mt-3 text-xs text-warn">{t("Keep scanlator, source and volume out of file names: upgrades replace files in place, and changing names later breaks read progress in Komga/Kavita.")}</p>
          </Card>
          <Card title={t("Library files")} className="mb-6">
            <div className="grid gap-4 md:grid-cols-2">
              <Switch env={lock("writeSeriesJson")} checked={m.writeSeriesJson} onChange={(v) => patch({ writeSeriesJson: v })} label={t("Write series.json (Komga series metadata)")} />
              <Switch env={lock("writeCover")} checked={m.writeCover} onChange={(v) => patch({ writeCover: v })} label={t("Write cover.jpg")} />
              <Switch env={lock("writeVolume")} checked={m.writeVolume} onChange={(v) => patch({ writeVolume: v })} label={t("Write volume numbers into ComicInfo.xml")} />
              <Switch
                env={lock("renameFolderOnTitleChange")}
                checked={m.renameFolderOnTitleChange}
                onChange={(v) => patch({ renameFolderOnTitleChange: v })}
                label={t("Rename a series folder when its title changes")}
              />
              <Field env={lock("minFreeSpaceMb")} label={t("Minimum free space (MB)")} help={t("Downloads pause when a root folder has less.")}>
                <Input type="number" value={m.minFreeSpaceMb} onChange={(e) => patch({ minFreeSpaceMb: Number(e.target.value) })} />
              </Field>
              <Field env={lock("fileMode")} label={t("File permissions")} help={t("Octal, e.g. 0664")}>
                <Input value={m.fileMode} onChange={(e) => patch({ fileMode: e.target.value })} />
              </Field>
              <Field env={lock("dirMode")} label={t("Folder permissions")}>
                <Input value={m.dirMode} onChange={(e) => patch({ dirMode: e.target.value })} />
              </Field>
            </div>
          </Card>
          <Card title={t("Recycle bin")}>
            <div className="grid gap-4 md:grid-cols-2">
              <Field env={lock("recycleBinPath")} label={t("Recycle bin folder")} help={t("Replaced and cleaned files go here. Empty = inside the data folder.")}>
                <Input value={m.recycleBinPath} onChange={(e) => patch({ recycleBinPath: e.target.value })} />
              </Field>
              <Field env={lock("recycleBinDays")} label={t("Keep recycled files (days)")} help={t("0 = forever")}>
                <Input type="number" value={m.recycleBinDays} onChange={(e) => patch({ recycleBinDays: Number(e.target.value) })} />
              </Field>
            </div>
          </Card>
        </>
      )}
      <SaveBar dirty={dirty} saving={saving} onSave={() => void save()} onDiscard={reset} />
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
  const [relocating, setRelocating] = useState<{ id: number; path: string; moveFiles: boolean } | null>(null);
  const relocate = async () => {
    if (!relocating) return;
    try {
      await unwrap(api.PUT("/api/v1/rootfolders/{id}", { params: { path: { id: relocating.id } }, body: { path: relocating.path, moveFiles: relocating.moveFiles } }));
      toast.success(relocating.moveFiles ? "Moving the root folder in the background" : "Root folder location updated");
      setRelocating(null);
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
    <Card title={t("Root folders")} className="mb-6">
      {relocating && (
        <Modal
          open
          onClose={() => setRelocating(null)}
          title={t("Change root folder location")}
          footer={
            <>
              <Button onClick={() => setRelocating(null)}>{t("Cancel")}</Button>
              <Button variant="primary" onClick={relocate}>
                {relocating.moveFiles ? tr("Move series") : tr("Update path")}
              </Button>
            </>
          }
        >
          <Field label={t("New path")}>
            <Input value={relocating.path} onChange={(e) => setRelocating({ ...relocating, path: e.target.value })} />
          </Field>
          <div className="mt-3">
            <Switch
              checked={relocating.moveFiles}
              onChange={(v) => setRelocating({ ...relocating, moveFiles: v })}
              label={t("Move the series folders there (off: they were already moved)")}
            />
          </div>
          <p className="mt-2 text-xs text-muted">{t("Update library server path mappings if they point at the old location.")}</p>
        </Modal>
      )}
      {isLoading && <Loading />}
      {data && data.length > 0 && (
        <Table className="mb-4">
          <thead>
            <tr>
              <Th>{t("Path")}</Th>
              <Th>{t("Language")}</Th>
              <Th>{t("Series")}</Th>
              <Th>{t("Free space")}</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((r) => (
              <tr key={r.id}>
                <Td className="font-mono text-xs">
                  {r.path} {r.managedBy && <EnvLock env="MANGARR_ROOT_FOLDERS" />}
                </Td>
                <Td>{r.language || "—"}</Td>
                <Td>{r.seriesCount}</Td>
                <Td>{r.accessible ? bytes(r.freeSpace) : <Badge tone="err">{r.error}</Badge>}</Td>
                <Td className="text-right">
                  <IconButton title={t("Change location")} disabled={!!r.managedBy} onClick={() => setRelocating({ id: r.id, path: r.path, moveFiles: true })}>
                    <FolderInput className="size-4" />
                  </IconButton>
                  <IconButton title={r.managedBy ? tr("Set by MANGARR_ROOT_FOLDERS") : tr("Remove")} onClick={() => remove(r.id)} disabled={r.seriesCount > 0 || !!r.managedBy}>
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
        <Button icon={<FolderPlus className="size-4" />} disabled={!path} onClick={add}>{t("Add root folder")}</Button>
      </div>
      <p className="mt-2 text-xs text-muted">{t("Use one root folder per language. Mount the same folder read-only into Komga/Kavita.")}</p>
    </Card>
  );
}
