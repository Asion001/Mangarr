import { t as tr, t } from "../../lib/i18n/core";
import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { ArrowRight, Pencil, Plus, Trash2 } from "lucide-react";
import { api, apiUrl, unwrap, type Profile, type S } from "../../api/client";
import { useChapters, useModules, useProfiles, useSeriesList } from "../../api/queries";
import { Badge, Button, Card, Confirm, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, Segmented, Select, Switch, TagInput } from "../../components/ui";
import { bytes } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useSettingsDoc } from "./useSettingsDoc";

type Cfg = Profile["config"];
type Tab = "releases" | "processing" | "cleanup";
type Preset = Cfg["encode"]["preset"];

const emptyConfig: Cfg = {
  preferredScanlators: [],
  blockedScanlators: [],
  allowUpgrades: false,
  minPages: 0,
  upscale: { enabled: false, upscalerId: 0, minWidth: 1400, maxWidth: 2048, model: "waifu2x-cunet", noise: 1, format: "source", quality: 90 },
  encode: { format: "keep", preset: "balanced", quality: 0, speed: 0, grayscale: true, progressive: false, minSavingsPct: 10, recycleOriginals: true },
  processTiming: "background",
  processExisting: false,
  cleanup: {},
};

// AVIF quality each speed preset uses when Quality is left empty (imageenc.Resolve)
const presetQuality: Record<Preset, number> = { fast: 60, balanced: 55, max: 48 };

// reader support for re-encoded pages (see docs/setup.md)
const compat: Record<string, { yes: string; no: string; note?: string }> = {
  avif: {
    yes: "Mihon 0.17+, Tachimanga, Panels (iOS 17+), Paperback (iOS 16+), Komga, Kavita",
    no: "KOReader",
    note: "Chunky works through Komga's OPDS; 32-bit ARM Komga can't read AVIF.",
  },
  jxl: { yes: "Mihon 0.17+, Tachimanga, Panels (iOS 17+), Komga", no: "Kavita, KOReader", note: "JPEG pages can be restored bit for bit." },
};

/** normalize fills defaults and maps older upscale formats onto "Save pages as". */
function normalize(profile: Profile): Profile {
  const upscale = { ...emptyConfig.upscale, ...profile.config.upscale };
  // upscaled pages keep their own format unless they're re-encoded; the
  // separate WebP/JPEG/PNG choice is gone
  upscale.format = "source";
  const encode = { ...emptyConfig.encode, ...profile.config.encode };
  return { ...profile, config: { ...emptyConfig, ...profile.config, upscale, encode } };
}

const formatName = (f: Cfg["encode"]["format"]) => (f === "avif" ? "AVIF" : f === "jxl" ? "JPEG XL" : "");
const presetName = (p: Preset) => (p === "fast" ? tr("Fast") : p === "max" ? tr("Smallest") : tr("Balanced"));

/** processingSummary is the one-line pipeline of a profile, e.g. "Upscale under 1400 px → AVIF". */
function processingSummary(c: Cfg) {
  const steps = [];
  if (c.upscale.enabled) steps.push(tr("Upscale under {px} px", { px: c.upscale.minWidth }));
  if (c.encode?.format && c.encode.format !== "keep") steps.push(formatName(c.encode.format));
  return steps.length ? steps.join(" → ") : tr("Pages as downloaded");
}

function profileSummary(c: Cfg) {
  return [
    processingSummary(c),
    c.allowUpgrades ? tr("upgrades on") : tr("upgrades off"),
    c.preferredScanlators?.length ? tr("prefers {names}", { names: c.preferredScanlators.join(", ") }) : "",
    c.blockedScanlators?.length ? tr("blocks {names}", { names: c.blockedScanlators.join(", ") }) : "",
    c.cleanup?.enabled === false ? tr("no cleanup") : c.cleanup?.enabled ? tr("own cleanup") : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

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
            <p className="text-sm text-muted">{profileSummary(normalize(p).config)}</p>
          </Card>
        ))}
      </div>
      {editing && <ProfileEditor profile={editing} onClose={() => setEditing(null)} />}
      <Confirm
        open={!!deleting}
        title={t("Delete profile")}
        danger
        confirmLabel={t("Delete")}
        message={t("Delete {name}?", { name: deleting?.name ?? "" })}
        onConfirm={remove}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}

/** Step is one numbered stage of page processing. */
function Step({ n, title, hint, action, children }: { n: number; title: string; hint?: string; action?: ReactNode; children?: ReactNode }) {
  return (
    <section className="rounded-lg border border-border">
      <header className="flex items-center gap-3 px-3.5 py-3">
        <span className="flex size-5.5 shrink-0 items-center justify-center rounded-full bg-panel-2 text-xs font-bold text-muted">{n}</span>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold">{title}</h3>
          {hint && <p className="text-xs text-muted">{hint}</p>}
        </div>
        {action}
      </header>
      {children && <div className="px-3.5 pb-3.5 sm:pl-12">{children}</div>}
    </section>
  );
}

function ProfileEditor({ profile, onClose }: { profile: Profile; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: upscalers } = useModules("upscale");
  const globalCleanup = useSettingsDoc<S["Cleanup"]>("cleanup").value;
  const [base] = useState(() => normalize(profile));
  const [p, setP] = useState<Profile>(base);
  const [tab, setTab] = useState<Tab>("releases");
  const [previewing, setPreviewing] = useState(false);
  const [applyTo, setApplyTo] = useState<{ id: number; files: number; bytes: number } | null>(null);
  const [saving, setSaving] = useState(false);
  const cfg = p.config;
  const setCfg = (c: Partial<Cfg>) => setP({ ...p, config: { ...cfg, ...c } });
  const up = cfg.upscale;
  const setUp = (u: Partial<Cfg["upscale"]>) => setCfg({ upscale: { ...up, ...u } });
  const enc = cfg.encode;
  const setEnc = (e: Partial<Cfg["encode"]>) => setCfg({ encode: { ...enc, ...e } });
  const upscalerId = upscalers?.find((candidate) => candidate.enabled)?.id || 0;
  const { data: info } = useQuery({
    queryKey: ["upscaler-info", upscalerId],
    queryFn: () => unwrap(api.GET("/api/v1/modules/{id}/upscaler-info", { params: { path: { id: upscalerId } } })),
    enabled: upscalerId > 0 && up.enabled,
    retry: false,
  });

  const encoding = enc.format !== "keep";
  const processing = up.enabled || encoding;
  const changedProcessing = JSON.stringify([base.config.upscale, base.config.encode]) !== JSON.stringify([cfg.upscale, cfg.encode]);

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

  const cleanupMode = cfg.cleanup?.enabled === undefined || cfg.cleanup?.enabled === null ? "inherit" : cfg.cleanup.enabled ? "custom" : "never";
  const tabs: { value: Tab; label: string; hint: string }[] = [
    { value: "releases", label: tr("Releases"), hint: cfg.allowUpgrades ? tr("upgrades on") : tr("upgrades off") },
    { value: "processing", label: tr("Page processing"), hint: processingSummary(cfg) },
    { value: "cleanup", label: tr("Cleanup"), hint: cleanupMode === "inherit" ? tr("Library default") : cleanupMode === "never" ? tr("Never") : tr("Custom") },
  ];
  const saveAs: { value: Cfg["encode"]["format"]; name: string; desc: string }[] = [
    { value: "keep", name: tr("As downloaded"), desc: up.enabled ? tr("Upscaled pages keep their format; the rest stay untouched.") : tr("Pages stay exactly as the source sent them.") },
    { value: "avif", name: "AVIF", desc: tr("Smallest files, typically 40–70% less. Lossy.") },
    { value: "jxl", name: "JPEG XL", desc: tr("Lossless. About 20% less for JPEG pages.") },
  ];
  const c = compat[enc.format];

  return (
    <Modal
      open
      onClose={onClose}
      title={p.id ? t("Edit {name}", { name: profile.name }) : t("New profile")}
      size="xl"
      footer={
        <>
          <Button onClick={onClose}>{t("Cancel")}</Button>
          <Button variant="primary" loading={saving} onClick={save}>{t("Save")}</Button>
        </>
      }
    >
      <div className="mb-4 flex flex-wrap items-end gap-4">
        <Field label={t("Name")} className="min-w-56 flex-1">
          <Input value={p.name} onChange={(e) => setP({ ...p, name: e.target.value })} />
        </Field>
        <div className="pb-2">
          <Switch checked={p.isDefault} onChange={(v) => setP({ ...p, isDefault: v })} label={t("Default profile")} />
        </div>
      </div>
      <div className="flex flex-col gap-4 md:flex-row">
        <nav aria-label={t("Profile sections")} className="-mx-1 flex shrink-0 gap-1 overflow-x-auto md:mx-0 md:w-44 md:flex-col md:self-start">
          {tabs.map((x) => (
            <button
              key={x.value}
              type="button"
              aria-current={tab === x.value ? "page" : undefined}
              onClick={() => setTab(x.value)}
              className={clsx("flex min-w-0 flex-col rounded-md px-2.5 py-2 text-left", tab === x.value ? "bg-panel-2 text-fg" : "text-muted hover:text-fg")}
            >
              <span className={clsx("text-sm", tab === x.value ? "font-semibold" : "font-medium")}>{x.label}</span>
              <span className="truncate text-xs text-muted">{x.hint}</span>
            </button>
          ))}
        </nav>
        <div className="min-w-0 flex-1">
          {tab === "releases" && (
            <div className="flex flex-col gap-4">
              <Field label={<>{t("Preferred scanlators")} <span className="font-normal text-muted">· {t("first wins")}</span></>} help={t("Names or regular expressions. Sources are ranked by series priority before scanlators.")}>
                <TagInput value={cfg.preferredScanlators ?? []} onChange={(v) => setCfg({ preferredScanlators: v })} placeholder={t("e.g. ^Official$ or TCB")} />
              </Field>
              <Field label={t("Never download from")} help={t("Releases matching these are never downloaded.")}>
                <TagInput value={cfg.blockedScanlators ?? []} onChange={(v) => setCfg({ blockedScanlators: v })} placeholder={t("Add a name or pattern…")} />
              </Field>
              <Switch checked={cfg.allowUpgrades} onChange={(v) => setCfg({ allowUpgrades: v })} label={t("Replace a chapter when a better release appears")} />
              <label className="flex flex-wrap items-center gap-2 text-sm">
                {t("Skip chapters under")}
                <Input
                  className="w-20"
                  type="number"
                  min={0}
                  placeholder={t("any")}
                  value={cfg.minPages || ""}
                  onChange={(e) => setCfg({ minPages: Number(e.target.value) || 0 })}
                />
                {t("pages")}
              </label>
            </div>
          )}

          {tab === "processing" && (
            <div className="flex flex-col gap-4">
              <div role="img" aria-label={t("What happens to each downloaded page")} className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-bg px-3.5 py-3 text-sm">
                <span className="rounded bg-panel-2 px-2.5 py-1 text-fg/80">{t("Downloaded page")}</span>
                {up.enabled && (
                  <>
                    <ArrowRight className="size-4 text-muted" />
                    <span className="rounded bg-info/15 px-2.5 py-1 text-info">{t("Upscale if narrower than {px} px", { px: up.minWidth })}</span>
                  </>
                )}
                <ArrowRight className="size-4 text-muted" />
                <span className={clsx("rounded px-2.5 py-1", encoding ? "bg-accent/15 text-accent-2" : "bg-panel-2 text-fg/80")}>
                  {encoding
                    ? t("Save as {format} · {speed}", { format: formatName(enc.format), speed: presetName(enc.preset).toLowerCase() })
                    : up.enabled
                      ? t("Keep each page's format")
                      : t("Saved as downloaded")}
                </span>
                <span className="flex-1" />
                {processing && <span className="text-xs text-muted">{cfg.processTiming === "inline" ? t("before import") : t("in the background")}</span>}
                {encoding && <Button size="sm" onClick={() => setPreviewing(true)}>{t("Preview on a chapter…")}</Button>}
              </div>

              <Step
                n={1}
                title={t("Upscale small pages")}
                hint={up.enabled ? t("Wider pages skip this step.") : t("Off. Turn on to sharpen low-resolution scans.")}
                action={<Switch checked={up.enabled} onChange={(v) => setUp({ enabled: v })} label={<span className="sr-only">{t("Upscale small pages")}</span>} />}
              >
                {up.enabled && (
                  <div className="grid gap-4 md:grid-cols-3">
                    <Field label={t("Model")} help={info?.devices?.length ? `GPU: ${info.devices.join(", ")}` : undefined}>
                      <Select value={up.model} onChange={(e) => setUp({ model: e.target.value })}>
                        {(info?.models ?? [{ name: up.model, description: "" }]).map((m) => (
                          <option key={m.name} value={m.name} title={m.description}>
                            {m.name}
                          </option>
                        ))}
                      </Select>
                    </Field>
                    <Field label={t("When narrower than (px)")} help={t("iPad portrait: ~1600–2000")}>
                      <Input type="number" min={1} value={up.minWidth} onChange={(e) => setUp({ minWidth: Number(e.target.value) })} />
                    </Field>
                    <Field label={t("Then shrink to at most (px)")} help={t("Empty = no limit")}>
                      <Input type="number" min={0} placeholder={t("no limit")} value={up.maxWidth || ""} onChange={(e) => setUp({ maxWidth: Number(e.target.value) || 0 })} />
                    </Field>
                    <div className="flex flex-wrap items-center gap-3 md:col-span-3">
                      <span className="text-sm font-medium">{t("Noise reduction")}</span>
                      <Segmented
                        label={t("Noise reduction")}
                        value={up.noise}
                        onChange={(noise) => setUp({ noise })}
                        options={[{ value: -1, label: t("Off") }, ...[0, 1, 2, 3].map((n) => ({ value: n, label: String(n) }))]}
                      />
                      <span className="text-xs text-muted">{t("Removes JPEG artefacts from scans")}</span>
                    </div>
                  </div>
                )}
              </Step>

              <Step n={2} title={t("Save pages as")} hint={encoding && up.enabled ? t("Upscaled pages go straight into it, lossless.") : undefined}>
                <div role="radiogroup" aria-label={t("Save pages as")} className="grid gap-2 sm:grid-cols-3">
                  {saveAs.map((f) => (
                    <label key={f.value} className={clsx("flex cursor-pointer flex-col gap-1 rounded-md border p-2.5", enc.format === f.value ? "border-accent bg-accent/8" : "border-border hover:border-muted")}>
                      <span className="flex items-center gap-2 text-sm font-semibold">
                        <input
                          type="radio"
                          name="save-as"
                          className="accent-accent"
                          checked={enc.format === f.value}
                          onChange={() => setEnc({ format: f.value, progressive: false })}
                        />
                        {f.name}
                      </span>
                      <span className="text-xs text-muted">{f.desc}</span>
                    </label>
                  ))}
                </div>
                {!encoding && up.enabled && (
                  <Field label={t("Upscaled page quality")} help={t("For JPEG and WebP pages; PNG stays lossless.")} className="mt-4 max-w-56">
                    <Input type="number" min={1} max={100} value={up.quality} onChange={(e) => setUp({ quality: Number(e.target.value) })} />
                  </Field>
                )}
                {encoding && (
                  <div className="mt-4 grid gap-4 rounded-md bg-bg p-3.5 md:grid-cols-2">
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 md:col-span-2">
                      <span className="text-sm font-medium">{t("Speed")}</span>
                      <Segmented
                        label={t("Speed")}
                        value={enc.preset}
                        onChange={(preset) => setEnc({ preset })}
                        options={(["fast", "balanced", "max"] as const).map((v) => ({ value: v, label: presetName(v) }))}
                      />
                      <span className="text-xs text-muted">{t("Smallest is much slower")}</span>
                    </div>
                    {enc.format === "avif" && (
                      <Field label={t("Quality")} help={t("1–100, empty follows speed")}>
                        <Input
                          type="number"
                          min={0}
                          max={100}
                          placeholder={t("Auto ({q})", { q: presetQuality[enc.preset] })}
                          value={enc.quality || ""}
                          onChange={(e) => setEnc({ quality: Number(e.target.value) || 0 })}
                        />
                      </Field>
                    )}
                    <Field label={t("Keep the original unless it saves (%)")} help={up.enabled ? t("Upscaled pages are always converted") : undefined}>
                      <Input type="number" min={0} max={90} value={enc.minSavingsPct} onChange={(e) => setEnc({ minSavingsPct: Number(e.target.value) })} />
                    </Field>
                    <div className="flex flex-col gap-2.5 md:col-span-2">
                      {enc.format === "avif" && (
                        <Switch checked={enc.grayscale} onChange={(v) => setEnc({ grayscale: v })} label={t("Store black-and-white pages without colour (smaller)")} />
                      )}
                      <Switch checked={enc.recycleOriginals} onChange={(v) => setEnc({ recycleOriginals: v })} label={t("Keep replaced originals in the recycle bin")} />
                    </div>
                    {c && (
                      <p className="border-t border-border pt-2.5 text-xs text-muted md:col-span-2">
                        <span className="font-semibold text-ok">{t("Opens in")}</span> {c.yes} · <span className="font-semibold text-warn">{t("Not in")}</span> {c.no}
                        {c.note && <> · {c.note}</>} · {t("mangarr checks Komga after the first chapter and pauses if it can't read it.")}
                      </p>
                    )}
                  </div>
                )}
              </Step>

              {processing && (
                <Step
                  n={3}
                  title={t("When")}
                  hint={t("In the background, chapters are readable right away; night hours are in Settings → Schedule.")}
                  action={
                    <Segmented
                      label={t("When")}
                      value={cfg.processTiming || "background"}
                      onChange={(processTiming) => setCfg({ processTiming })}
                      options={[
                        { value: "background", label: t("After import, in the background") },
                        { value: "inline", label: t("Before import") },
                      ]}
                    />
                  }
                />
              )}
            </div>
          )}

          {tab === "cleanup" && (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <h3 className="flex-1 text-sm font-semibold">{t("Cleanup of read chapters")}</h3>
                <Segmented
                  label={t("Cleanup of read chapters")}
                  value={cleanupMode}
                  onChange={(m) => setCfg({ cleanup: m === "inherit" ? {} : m === "never" ? { enabled: false } : { ...cfg.cleanup, enabled: true } })}
                  options={[
                    { value: "inherit", label: globalCleanup ? (globalCleanup.enabled ? t("Library default (on)") : t("Library default (off)")) : t("Library default") },
                    { value: "custom", label: t("Custom") },
                    { value: "never", label: t("Never") },
                  ]}
                />
              </div>
              {cleanupMode === "custom" && (
                <div className="grid gap-4 md:grid-cols-2">
                  <Field label={t("Keep the last read chapters")} help={globalCleanup ? t("Empty uses the library default ({n})", { n: globalCleanup.keepLastRead }) : undefined}>
                    <Input
                      type="number"
                      min={0}
                      placeholder={globalCleanup ? String(globalCleanup.keepLastRead) : ""}
                      value={cfg.cleanup?.keepLastRead ?? ""}
                      onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, keepLastRead: e.target.value === "" ? undefined : Number(e.target.value) } })}
                    />
                  </Field>
                  <Field label={t("Delete after (days)")} help={globalCleanup ? t("Empty uses the library default ({n})", { n: globalCleanup.graceDays }) : undefined}>
                    <Input
                      type="number"
                      min={0}
                      placeholder={globalCleanup ? String(globalCleanup.graceDays) : ""}
                      value={cfg.cleanup?.graceDays ?? ""}
                      onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, graceDays: e.target.value === "" ? undefined : Number(e.target.value) } })}
                    />
                  </Field>
                </div>
              )}
              {cleanupMode === "never" && <p className="text-sm text-muted">{t("Series using this profile keep every downloaded chapter.")}</p>}
            </div>
          )}
        </div>
      </div>
      {previewing && <EncodePreview encode={enc} onClose={() => setPreviewing(false)} />}
      <Confirm
        open={!!applyTo}
        title={t("Process existing chapters?")}
        confirmLabel={t("Process them")}
        message={
          applyTo
            ? t("{files} chapters ({size}) already downloaded with this profile weren't processed with these settings. Process them in the background too? New chapters are processed automatically either way.", {
                files: applyTo.files,
                size: bytes(applyTo.bytes),
              })
            : ""
        }
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
