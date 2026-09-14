import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type Profile } from "../../api/client";
import { useModules, useProfiles } from "../../api/queries";
import { Badge, Button, Card, Confirm, Field, IconButton, Input, Loading, Modal, PageHeader, Select, Switch, TagInput } from "../../components/ui";
import { useToast } from "../../lib/toast";

type Cfg = Profile["config"];

const emptyConfig: Cfg = {
  preferredScanlators: [],
  blockedScanlators: [],
  allowUpgrades: false,
  minPages: 0,
  upscale: { enabled: false, upscalerId: 0, minWidth: 1400, maxWidth: 2048, model: "waifu2x-cunet", noise: 1, format: "webp", quality: 90 },
  cleanup: {},
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
        title="Profiles"
        subtitle="How releases are chosen, upgraded, upscaled and cleaned for the series using a profile."
        actions={
          <Button
            variant="primary"
            icon={<Plus className="size-4" />}
            onClick={() => setEditing({ id: 0, name: "", isDefault: false, config: structuredClone(emptyConfig), createdAt: "", updatedAt: "" })}
          >
            Add profile
          </Button>
        }
      />
      {isLoading && <Loading />}
      <div className="grid gap-3 md:grid-cols-2">
        {data?.map((p) => (
          <Card
            key={p.id}
            title={
              <span className="flex items-center gap-2">
                {p.name} {p.isDefault && <Badge tone="accent">default</Badge>}
              </span>
            }
            actions={
              <>
                <IconButton title="Edit" onClick={() => setEditing(p)}>
                  <Pencil className="size-4" />
                </IconButton>
                {!p.isDefault && (
                  <IconButton title="Delete" onClick={() => setDeleting(p)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                )}
              </>
            }
          >
            <div className="flex flex-wrap gap-1.5 text-xs">
              <Badge tone={p.config.allowUpgrades ? "info" : "default"}>upgrades {p.config.allowUpgrades ? "on" : "off"}</Badge>
              <Badge tone={p.config.upscale.enabled ? "accent" : "default"}>upscale {p.config.upscale.enabled ? `< ${p.config.upscale.minWidth}px` : "off"}</Badge>
              {p.config.preferredScanlators?.length ? <Badge>prefers {p.config.preferredScanlators.join(", ")}</Badge> : null}
              {p.config.blockedScanlators?.length ? <Badge tone="err">blocks {p.config.blockedScanlators.join(", ")}</Badge> : null}
              {p.config.cleanup?.enabled !== undefined && <Badge tone="warn">cleanup {p.config.cleanup.enabled ? "on" : "off"}</Badge>}
            </div>
          </Card>
        ))}
      </div>
      {editing && <ProfileEditor profile={editing} onClose={() => setEditing(null)} />}
      <Confirm open={!!deleting} title="Delete profile" danger confirmLabel="Delete" message={`Delete ${deleting?.name}?`} onConfirm={remove} onClose={() => setDeleting(null)} />
    </>
  );
}

function ProfileEditor({ profile, onClose }: { profile: Profile; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: upscalers } = useModules("upscale");
  const [p, setP] = useState<Profile>(() => ({ ...profile, config: { ...emptyConfig, ...profile.config, upscale: { ...emptyConfig.upscale, ...profile.config.upscale } } }));
  const [saving, setSaving] = useState(false);
  const cfg = p.config;
  const setCfg = (c: Partial<Cfg>) => setP({ ...p, config: { ...cfg, ...c } });
  const up = cfg.upscale;
  const setUp = (u: Partial<Cfg["upscale"]>) => setCfg({ upscale: { ...up, ...u } });
  const upscalerId = up.upscalerId || upscalers?.[0]?.id || 0;
  const { data: info } = useQuery({
    queryKey: ["upscaler-info", upscalerId],
    queryFn: () => unwrap(api.GET("/api/v1/modules/{id}/upscaler-info", { params: { path: { id: upscalerId } } })),
    enabled: upscalerId > 0 && up.enabled,
    retry: false,
  });

  const save = async () => {
    setSaving(true);
    try {
      if (p.id) await unwrap(api.PUT("/api/v1/profiles/{id}", { params: { path: { id: p.id } }, body: p }));
      else await unwrap(api.POST("/api/v1/profiles", { body: p }));
      qc.invalidateQueries({ queryKey: ["profiles"] });
      toast.success("Profile saved");
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
      title={p.id ? `Edit ${profile.name}` : "New profile"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={saving} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Name">
            <Input value={p.name} onChange={(e) => setP({ ...p, name: e.target.value })} />
          </Field>
          <div className="flex items-end">
            <Switch checked={p.isDefault} onChange={(v) => setP({ ...p, isDefault: v })} label="Default profile" />
          </div>
        </div>
        <h3 className="font-semibold">Release selection</h3>
        <Field label="Preferred scanlators" help="Regular expressions, most preferred first. Sources are ranked by series priority before scanlators.">
          <TagInput value={cfg.preferredScanlators ?? []} onChange={(v) => setCfg({ preferredScanlators: v })} placeholder="e.g. ^Official$ or TCB" />
        </Field>
        <Field label="Blocked scanlators" help="Releases matching these are never downloaded.">
          <TagInput value={cfg.blockedScanlators ?? []} onChange={(v) => setCfg({ blockedScanlators: v })} />
        </Field>
        <div className="grid gap-4 md:grid-cols-2">
          <Switch checked={cfg.allowUpgrades} onChange={(v) => setCfg({ allowUpgrades: v })} label="Upgrade chapters when a better release appears" />
          <Field label="Minimum pages" help="0 = off">
            <Input type="number" min={0} value={cfg.minPages} onChange={(e) => setCfg({ minPages: Number(e.target.value) })} />
          </Field>
        </div>

        <h3 className="font-semibold">Upscaling</h3>
        <Switch checked={up.enabled} onChange={(v) => setUp({ enabled: v })} label="Upscale small pages" />
        {up.enabled && (
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Upscaler">
              <Select value={upscalerId} onChange={(e) => setUp({ upscalerId: Number(e.target.value) })}>
                {upscalers?.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}
                  </option>
                ))}
                {!upscalers?.length && <option value={0}>No upscaler configured</option>}
              </Select>
            </Field>
            <Field label="Model" help={info?.devices?.length ? `GPU: ${info.devices.join(", ")}` : undefined}>
              <Select value={up.model} onChange={(e) => setUp({ model: e.target.value })}>
                {(info?.models ?? [{ name: up.model, description: "" }]).map((m) => (
                  <option key={m.name} value={m.name} title={m.description}>
                    {m.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Upscale pages narrower than (px)" help="iPad portrait: ~1600-2000px. Wider pages are left untouched.">
              <Input type="number" value={up.minWidth} onChange={(e) => setUp({ minWidth: Number(e.target.value) })} />
            </Field>
            <Field label="Maximum width (px)" help="Results are downscaled to at most this width. 0 = no cap.">
              <Input type="number" value={up.maxWidth} onChange={(e) => setUp({ maxWidth: Number(e.target.value) })} />
            </Field>
            <Field label="Noise reduction" help="-1 none … 3 strong (waifu2x / Real-CUGAN)">
              <Input type="number" min={-1} max={3} value={up.noise} onChange={(e) => setUp({ noise: Number(e.target.value) })} />
            </Field>
            <Field label="Output format">
              <Select value={up.format} onChange={(e) => setUp({ format: e.target.value })}>
                <option value="webp">WebP</option>
                <option value="jpeg">JPEG</option>
                <option value="png">PNG (large)</option>
              </Select>
            </Field>
            <Field label="Quality">
              <Input type="number" min={1} max={100} value={up.quality} onChange={(e) => setUp({ quality: Number(e.target.value) })} />
            </Field>
          </div>
        )}

        <h3 className="font-semibold">Cleanup overrides</h3>
        <div className="grid gap-4 md:grid-cols-3">
          <Field label="Cleanup">
            <Select
              value={tri(cfg.cleanup?.enabled)}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, enabled: e.target.value === "inherit" ? undefined : e.target.value === "on" } })}
            >
              <option value="inherit">Use global setting</option>
              <option value="on">Enabled</option>
              <option value="off">Disabled</option>
            </Select>
          </Field>
          <Field label="Keep last read" help="empty = global">
            <Input
              type="number"
              value={cfg.cleanup?.keepLastRead ?? ""}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, keepLastRead: e.target.value === "" ? undefined : Number(e.target.value) } })}
            />
          </Field>
          <Field label="Grace days" help="empty = global">
            <Input
              type="number"
              value={cfg.cleanup?.graceDays ?? ""}
              onChange={(e) => setCfg({ cleanup: { ...cfg.cleanup, graceDays: e.target.value === "" ? undefined : Number(e.target.value) } })}
            />
          </Field>
        </div>
      </div>
    </Modal>
  );
}
