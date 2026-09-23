import { t as tr, t } from "../../lib/i18n/core";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { api, apiUrl, unwrap, type Profile } from "../../api/client";
import { useChapters, useModules, useProfiles, useSeriesList } from "../../api/queries";
import { Badge, Button, Card, Confirm, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, Select, Switch, TagInput } from "../../components/ui";
import { bytes } from "../../lib/format";
import { useToast } from "../../lib/toast";

type Cfg = Profile["config"];

const emptyConfig: Cfg = {
  preferredScanlators: [],
  blockedScanlators: [],
  allowUpgrades: false,
  minPages: 0,
  upscale: { enabled: false, upscalerId: 0, minWidth: 1400, maxWidth: 2048, model: "waifu2x-cunet", noise: 1, format: "webp", quality: 90 },
  encode: { format: "keep", preset: "balanced", quality: 0, speed: 0, grayscale: true, progressive: false, minSavingsPct: 10, recycleOriginals: true },
  processTiming: "background",
  processExisting: false,
  cleanup: {},
};

// reader support for re-encoded pages (see docs/setup.md)
const compat: Record<string, { yes: string[]; no: string[]; note?: string }> = {
  avif: {
    yes: ["Mihon 0.17+", "Tachimanga", "Panels (iOS 17+)", "Paperback (iOS 16+)", "Komga (official amd64/arm64 image)", "Kavita"],
    no: ["KOReader"],
    note: "Chunky works through Komga's OPDS (Komga converts pages to JPEG). 32-bit ARM Komga can't read AVIF.",
  },
  jxl: {
    yes: ["Mihon 0.17+", "Tachimanga", "Panels (iOS 17+)", "Komga (official amd64/arm64 image)"],
    no: ["Kavita", "KOReader"],
    note: "Lossless: JPEG pages can be restored bit for bit.",
  },
};

export function ProfilesPage() {
  const { data, isLoading } = useProfiles();
  const qc = useQueryClient();
  const toast = useToast();
  const [editing, setEditing] = useState<Profile | null>(null);
  const [deleting, setDeleting] = useState<Profile | null>(null);

  const remove = async () => {
    if (!deleting) return;
    try {
      await unwrap(api.DELETE("/api/v1/profiles/{id}", { params: { path: { id: deleting.id } } }));
      qc.invalidateQueries({ queryKey: ["profiles"] });
      setDeleting(null);
    } catch (e) {
      toast.fromError(e);
    }
  };

  return (
    <>
      <PageHeader
        title={t("Profiles")}
        subtitle={t("How releases are chosen, upgraded, upscaled and cleaned for the series using a profile.")}
        actions={
          <Button
            variant="primary"
            icon={<Plus className="size-4" />}
            onClick={() => setEditing({ id: 0, name: "", isDefault: false, config: structuredClone(emptyConfig), createdAt: "", updatedAt: "" })}
          >{t("Add profile")}</Button>
        }
      />
      {isLoading && <Loading />}
      <div className="grid gap-3 md:grid-cols-2">
        {data?.map((p) => (
          <Card
            key={p.id}
            title={
              <span className="flex items-center gap-2">
                {p.name} {p.isDefault && <Badge tone="accent">{t("default")}</Badge>}
              </span>
            }
            actions={
              <>
                <IconButton title={t("Edit")} onClick={() => setEditing(p)}>
                  <Pencil className="size-4" />
                </IconButton>
                {!p.isDefault && (
                  <IconButton title={t("Delete")} onClick={() => setDeleting(p)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                )}
              </>
            }
          >
            <div className="flex flex-wrap gap-1.5 text-xs">
              <Badge tone={p.config.allowUpgrades ? "info" : "default"}>{t("upgrades") + " "}{p.config.allowUpgrades ? tr("on") : tr("off")}</Badge>
              <Badge tone={p.config.upscale.enabled ? "accent" : "default"}>{t("upscale") + " "}{p.config.upscale.enabled ? `< ${p.config.upscale.minWidth}px` : tr("off")}</Badge>
              {p.config.encode?.format && p.config.encode.format !== "keep" && <Badge tone="accent">{t("re-encode") + " "}{p.config.encode.format}</Badge>}
              {p.config.preferredScanlators?.length ? <Badge>{t("prefers") + " "}{p.config.preferredScanlators.join(", ")}</Badge> : null}
              {p.config.blockedScanlators?.length ? <Badge tone="err">{t("blocks") + " "}{p.config.blockedScanlators.join(", ")}</Badge> : null}
              {p.config.cleanup?.enabled !== undefined && <Badge tone="warn">{t("cleanup") + " "}{p.config.cleanup.enabled ? tr("on") : tr("off")}</Badge>}
            </div>
          </Card>
        ))}
      </div>
      {editing && <ProfileEditor profile={editing} onClose={() => setEditing(null)} />}
      <Confirm open={!!deleting} title={t("Delete profile")} danger confirmLabel={t("Delete")} message={`Delete ${deleting?.name}?`} onConfirm={remove} onClose={() => setDeleting(null)} />
    </>
  );
}

function ProfileEditor({ profile, onClose }: { profile: Profile; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: upscalers } = useModules("upscale");
  const [p, setP] = useState<Profile>(() => ({
    ...profile,
    config: { ...emptyConfig, ...profile.config, upscale: { ...emptyConfig.upscale, ...profile.config.upscale }, encode: { ...emptyConfig.encode, ...profile.config.encode } },
  }));
  const [previewing, setPreviewing] = useState(false);
  const [applyTo, setApplyTo] = useState<{ id: number; files: number; bytes: number } | null>(null);
  const [saving, setSaving] = useState(false);
  const cfg = p.config;
  const setCfg = (c: Partial<Cfg>) => setP({ ...p, config: { ...cfg, ...c } });
  const up = cfg.upscale;
  const setUp = (u: Partial<Cfg["upscale"]>) => setCfg({ upscale: { ...up, ...u } });
  const upscalerId = upscalers?.find((candidate) => candidate.enabled)?.id || 0;
  const { data: info } = useQuery({
    queryKey: ["upscaler-info", upscalerId],
    queryFn: () => unwrap(api.GET("/api/v1/modules/{id}/upscaler-info", { params: { path: { id: upscalerId } } })),
    enabled: upscalerId > 0 && up.enabled,
    retry: false,
  });

  const enc = cfg.encode;
  const setEnc = (e: Partial<Cfg["encode"]>) => setCfg({ encode: { ...enc, ...e } });
  const processing = up.enabled || (enc.format && enc.format !== "keep");
  const changedProcessing = JSON.stringify([profile.config.upscale, profile.config.encode]) !== JSON.stringify([cfg.upscale, cfg.encode]);

  const save = async () => {
    setSaving(true);
    try {
      const saved = p.id
        ? await unwrap(api.PUT("/api/v1/profiles/{id}", { params: { path: { id: p.id } }, body: p }))
        : await unwrap(api.POST("/api/v1/profiles", { body: p }));
      qc.invalidateQueries({ queryKey: ["profiles"] });
      toast.success(tr("Profile saved"));
      if (processing && changedProcessing && !cfg.processExisting && p.id) {
        const est = await unwrap(api.GET("/api/v1/profiles/{id}/process-estimate", { params: { path: { id: saved.id } } }));
        if (est.files > 0) {
          setApplyTo({ id: saved.id, files: est.files, bytes: est.bytes });
          return;
        }
      }
      onClose();
    } catch (e) {
      toast.fromError(e);
    } finally {
      setSaving(false);
    }
  };
  const tri = (v?: boolean) => (v === undefined || v === null ? "inherit" : v ? "on" : "off");

  return (
    <Modal
      open
      onClose={onClose}
      title={p.id ? `Edit ${profile.name}` : tr("New profile")}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>{t("Cancel")}</Button>
          <Button variant="primary" loading={saving} onClick={save}>{t("Save")}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="grid gap-4 md:grid-cols-2">
          <Field label={t("Name")}>
            <Input value={p.name} onChange={(e) => setP({ ...p, name: e.target.value })} />
          </Field>
          <div className="flex items-end">
            <Switch checked={p.isDefault} onChange={(v) => setP({ ...p, isDefault: v })} label={t("Default profile")} />
          </div>
        </div>
        <h3 className="font-semibold">{t("Release selection")}</h3>
        <Field label={t("Preferred scanlators")} help={t("Regular expressions, most preferred first. Sources are ranked by series priority before scanlators.")}>
          <TagInput value={cfg.preferredScanlators ?? []} onChange={(v) => setCfg({ preferredScanlators: v })} placeholder={t("e.g. ^Official$ or TCB")} />
        </Field>
        <Field label={t("Blocked scanlators")} help={t("Releases matching these are never downloaded.")}>
          <TagInput value={cfg.blockedScanlators ?? []} onChange={(v) => setCfg({ blockedScanlators: v })} />
        </Field>
        <div className="grid gap-4 md:grid-cols-2">
          <Switch checked={cfg.allowUpgrades} onChange={(v) => setCfg({ allowUpgrades: v })} label={t("Upgrade chapters when a better release appears")} />
          <Field label={t("Minimum pages")} help={t("0 = off")}>
            <Input type="number" min={0} value={cfg.minPages} onChange={(e) => setCfg({ minPages: Number(e.target.value) })} />
          </Field>
        </div>

        <h3 className="font-semibold">{t("Processing")}</h3>
        <Field label={t("When")} help={t("Background: chapters are readable right away and processed later (e.g. at night, see Settings → Schedule).")}>
          <Select value={cfg.processTiming || "background"} onChange={(e) => setCfg({ processTiming: e.target.value as Cfg["processTiming"] })}>
            <option value="background">{t("In the background, after import")}</option>
            <option value="inline">{t("Before import (slower to appear)")}</option>
          </Select>
        </Field>
        <h3 className="font-semibold">{t("Upscaling")}</h3>
        <Switch checked={up.enabled} onChange={(v) => setUp({ enabled: v })} label={t("Upscale small pages")} />
        {up.enabled && (
          <div className="grid gap-4 md:grid-cols-2">
            <Field label={t("Model")} help={info?.devices?.length ? `GPU: ${info.devices.join(", ")}` : undefined}>
              <Select value={up.model} onChange={(e) => setUp({ model: e.target.value })}>
                {(info?.models ?? [{ name: up.model, description: "" }]).map((m) => (
                  <option key={m.name} value={m.name} title={m.description}>
                    {m.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label={t("Upscale pages narrower than (px)")} help={t("iPad portrait: ~1600-2000px. Wider pages are left untouched.")}>
              <Input type="number" value={up.minWidth} onChange={(e) => setUp({ minWidth: Number(e.target.value) })} />
            </Field>
            <Field label={t("Maximum width (px)")} help={t("Results are downscaled to at most this width. 0 = no cap.")}>
              <Input type="number" value={up.maxWidth} onChange={(e) => setUp({ maxWidth: Number(e.target.value) })} />
            </Field>
            <Field label={t("Noise reduction")} help={t("-1 none … 3 strong (waifu2x / Real-CUGAN)")}>
              <Input type="number" min={-1} max={3} value={up.noise} onChange={(e) => setUp({ noise: Number(e.target.value) })} />
            </Field>
            <Field label={t("Output format")}>
              <Select value={up.format} onChange={(e) => setUp({ format: e.target.value })}>
                <option value="webp">WebP</option>
                <option value="jpeg">JPEG</option>
                <option value="png">{t("PNG (large)")}</option>
              </Select>
            </Field>
            <Field label={t("Quality")}>
              <Input type="number" min={1} max={100} value={up.quality} onChange={(e) => setUp({ quality: Number(e.target.value) })} />
            </Field>
          </div>
        )}

        <h3 className="font-semibold">{t("Re-encoding to save space")}</h3>
        <div className="grid gap-4 md:grid-cols-2">
          <Field label={t("Format")}>
            <Select
              value={enc.format}
              onChange={(e) => {
                const format = e.target.value as Cfg["encode"]["format"];
                setEnc({ format, progressive: format === "avif" && enc.progressive });
              }}
            >
              <option value="keep">{t("Keep original pages")}</option>
              <option value="avif">{t("AVIF (lossy, typically 40-70% smaller)")}</option>
              <option value="jxl">{t("JPEG XL lossless (~20% smaller JPEGs, reversible)")}</option>
            </Select>
          </Field>
          {enc.format !== "keep" && (
            <Field label={t("Preset")} help={t("Max compression is much slower; try Preview or `mangarr bench encode` first.")}>
              <Select value={enc.preset} onChange={(e) => setEnc({ preset: e.target.value as Cfg["encode"]["preset"] })}>
                <option value="fast">{t("Fast")}</option>
                <option value="balanced">{t("Balanced")}</option>
                <option value="max">{t("Maximum compression")}</option>
              </Select>
            </Field>
          )}
        </div>
        {enc.format !== "keep" && compat[enc.format] && (
          <div className="rounded-md border border-border bg-panel-2 p-3 text-xs">
            <div>
              <span className="text-ok">{t("Reads") + " "}{enc.format.toUpperCase()}:</span> {compat[enc.format].yes.join(", ")}
            </div>
            <div className="mt-1">
              <span className="text-err">{t("Can't:")}</span> {compat[enc.format].no.join(", ")}
            </div>
            {compat[enc.format].note && <div className="mt-1 text-muted">{compat[enc.format].note}</div>}
            <div className="mt-1 text-muted">{t("After the first re-encoded chapter mangarr asks Komga whether it could read it, and pauses re-encoding if not.")}</div>
          </div>
        )}
        {enc.format !== "keep" && (
          <div className="grid gap-4 md:grid-cols-2">
            {enc.format === "avif" && (
              <Field label={t("Quality")} help={t("0 = preset (fast 60, balanced 55, max 48)")}>
                <Input type="number" min={0} max={100} value={enc.quality} onChange={(e) => setEnc({ quality: Number(e.target.value) })} />
              </Field>
            )}
            <Field label={t("Minimum saving per page (%)")} help={t("Pages that wouldn't shrink this much stay as they are")}>
              <Input type="number" min={0} max={90} value={enc.minSavingsPct} onChange={(e) => setEnc({ minSavingsPct: Number(e.target.value) })} />
            </Field>
            {enc.format === "avif" && <Switch checked={enc.grayscale} onChange={(v) => setEnc({ grayscale: v })} label={t("Encode black-and-white pages without color (smaller)")} />}
            {enc.format === "avif" && (
              <div>
                <Switch
                  checked={enc.progressive}
                  onChange={(v) => setEnc({ progressive: v })}
                  label={t("Show a low-detail AVIF preview while the page downloads")}
                />
                <p className="mt-1 text-xs text-muted">
                  {t("Requires the full image or avifenc 1.4+. Chrome renders the layers progressively; other compatible readers display the completed image normally.")}
                </p>
              </div>
            )}
            <Switch checked={enc.recycleOriginals} onChange={(v) => setEnc({ recycleOriginals: v })} label={t("Keep originals in the recycle bin for a while")} />
            <div className="md:col-span-2">
              <Button size="sm" onClick={() => setPreviewing(true)}>{t("Preview on a chapter…")}</Button>
            </div>
          </div>
        )}
        {processing && (
          <Switch
            checked={cfg.processExisting}
            onChange={(v) => setCfg({ processExisting: v })}
            label={t("Also process chapters downloaded before these settings changed")}
          />
        )}

        <h3 className="font-semibold">{t("Cleanup overrides")}</h3>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label={t("Cleanup")}>
            <Select
              value={tri(cfg.cleanup?.enabled)}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, enabled: e.target.value === "inherit" ? undefined : e.target.value === "on" } })}
            >
              <option value="inherit">{t("Use global setting")}</option>
              <option value="on">{t("Enabled")}</option>
              <option value="off">{t("Disabled")}</option>
            </Select>
          </Field>
          <Field label={t("Keep last read")} help={t("empty = global")}>
            <Input
              type="number"
              value={cfg.cleanup?.keepLastRead ?? ""}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, keepLastRead: e.target.value === "" ? undefined : Number(e.target.value) } })}
            />
          </Field>
          <Field label={t("Grace days")} help={t("empty = global")}>
            <Input
              type="number"
              value={cfg.cleanup?.graceDays ?? ""}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, graceDays: e.target.value === "" ? undefined : Number(e.target.value) } })}
            />
          </Field>
        </div>
      </div>
      {previewing && <EncodePreview encode={enc} onClose={() => setPreviewing(false)} />}
      <Confirm
        open={!!applyTo}
        title={t("Process existing chapters?")}
        confirmLabel={t("Process them")}
        message={applyTo ? `${applyTo.files} chapters (${bytes(applyTo.bytes)}) already downloaded with this profile weren't processed with these settings. Process them in the background too? New chapters are processed automatically either way.` : ""}
        onConfirm={async () => {
          if (!applyTo) return;
          try {
            await unwrap(api.PUT("/api/v1/profiles/{id}", { params: { path: { id: applyTo.id } }, body: { ...p, id: applyTo.id, config: { ...cfg, processExisting: true } } }));
            qc.invalidateQueries({ queryKey: ["profiles"] });
            toast.success(tr("Existing chapters will be processed in the background"));
          } catch (e) {
            toast.fromError(e);
          }
          setApplyTo(null);
          onClose();
        }}
        onClose={() => (setApplyTo(null), onClose())}
      />
    </Modal>
  );
}

/** EncodePreview re-encodes three pages of a chapter with the current settings. */
function EncodePreview({ encode, onClose }: { encode: Cfg["encode"]; onClose: () => void }) {
  const { data: series } = useSeriesList();
  const [seriesId, setSeriesId] = useState(0);
  const { data: chapters } = useChapters(seriesId);
  const withFiles = (chapters ?? []).filter((c) => c.file);
  const [chapterId, setChapterId] = useState(0);
  const run = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/processing/preview", { body: { chapterId: chapterId || withFiles[0]?.id, encode } })),
  });
  const res = run.data;
  const img = (i: number, v: "original" | "encoded") => apiUrl(`api/v1/processing/preview/${res!.token}/${i}/${v}`);
  return (
    <Modal open onClose={onClose} title={`Preview ${encode.format.toUpperCase()} (${encode.preset})`} size="xl">
      <div className="mb-4 flex flex-wrap items-end gap-2">
        <Field label={t("Series")} className="min-w-48 flex-1">
          <Select value={seriesId} onChange={(e) => (setSeriesId(Number(e.target.value)), setChapterId(0))}>
            <option value={0}>{t("Pick a series…")}</option>
            {series?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.title}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("Chapter")} className="w-40">
          <Select value={chapterId || withFiles[0]?.id || 0} onChange={(e) => setChapterId(Number(e.target.value))}>
            {withFiles.map((c) => (
              <option key={c.id} value={c.id}>
                {c.number}
              </option>
            ))}
          </Select>
        </Field>
        <Button variant="primary" disabled={!withFiles.length} loading={run.isPending} onClick={() => run.mutate()}>{t("Encode 3 pages")}</Button>
      </div>
      {run.error && <ErrorBox error={run.error} />}
      {res && (
        <>
          <p className="mb-3 text-sm text-muted">
            {res.engine} · {res.seconds.toFixed(1)}{" " + t("s ·")}{" "}
            <a className="text-accent-2 hover:underline" href={apiUrl(`api/v1/processing/preview/${res.token}/sample.cbz`)}>{t("download sample CBZ")}</a>{" "}{t("to check it in your reader app")}</p>
          <div className="flex flex-col gap-4">
            {res.pages.map((pg) => (
              <div key={pg.index} className="grid grid-cols-2 gap-2">
                {(["original", "encoded"] as const).map((v) => (
                  <figure key={v} className="flex flex-col gap-1">
                    <a href={img(pg.index, v)} target="_blank" rel="noreferrer">
                      <img src={img(pg.index, v)} alt={`${v} ${pg.name}`} className="w-full rounded border border-border" loading="lazy" />
                    </a>
                    <figcaption className="text-xs text-muted">
                      {v === "original" ? `${pg.originalFormat} · ${bytes(pg.originalSize)}` : `${pg.encodedFormat} · ${bytes(pg.encodedSize)} (${Math.round(100 - (100 * pg.encodedSize) / Math.max(pg.originalSize, 1))}% smaller)`}
                    </figcaption>
                  </figure>
                ))}
              </div>
            ))}
          </div>
        </>
      )}
    </Modal>
  );
}
