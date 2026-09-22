import { t as tr, t } from "../../lib/i18n/core";
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type ModuleResource, type S } from "../../api/client";
import { useModules } from "../../api/queries";
import { Badge, Button, Card, EmptyState, ErrorBox, IconButton, Input, Loading, Modal, PageHeader, Switch, Table, Td, Th } from "../../components/ui";
import { bytes, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

type Worker = S["WorkerResource"];

const roles = [
  { key: "download", label: "Download", help: "Fetches chapters from their source and uploads the pages here" },
  { key: "upscale", label: "Upscale", help: "Runs the upscaler on pages" },
  { key: "encode", label: "Encode", help: "Re-encodes pages to AVIF or JPEG XL" },
];

/** WorkersPage lists the machines that do work for this server. */
export function WorkersPage() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: engines, isLoading: enginesLoading, error: enginesError } = useModules("upscale");
  const { data, isLoading, error } = useQuery({
    queryKey: ["workers"],
    queryFn: () => unwrap(api.GET("/api/v1/workers")),
    refetchInterval: 15000,
  });
  const [adding, setAdding] = useState(false);
  const [issued, setIssued] = useState<{ name: string; key: string } | null>(null);
  const [removing, setRemoving] = useState<Worker | null>(null);

  const reload = () => qc.invalidateQueries({ queryKey: ["workers"] });
  const updateEngine = async (engine: ModuleResource, patch: { enabled?: boolean; priority?: number }) => {
    try {
      await unwrap(api.PUT("/api/v1/modules/{id}", {
        params: { path: { id: engine.id } },
        body: {
          kind: "upscale",
          implementation: engine.implementation,
          name: engine.name,
          enabled: patch.enabled ?? engine.enabled,
          priority: patch.priority ?? engine.priority,
          tags: engine.tags,
          events: engine.events,
          settings: engine.settings,
        },
      }));
      qc.invalidateQueries({ queryKey: ["modules", "upscale"] });
    } catch (e) {
      toast.fromError(e, tr("Could not update the processing engine"));
    }
  };
  const update = async (w: Worker, body: { enabled?: boolean; roles?: string[] }) => {
    try {
      await unwrap(api.PUT("/api/v1/workers/{id}", { params: { path: { id: w.id } }, body }));
      reload();
    } catch (e) {
      toast.fromError(e, tr("Could not update the worker"));
    }
  };

  return (
    <>
      <PageHeader
        title={t("Workers")}
        subtitle={t("Machines that download, upscale and encode for this server")}
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>{t("Add worker")}</Button>
        }
      />
      <Card title={t("Processing engines")} className="mb-6">
        <p className="mb-3 text-sm text-muted">{t("Profiles use the first available engine in this priority order.")}</p>
        {enginesLoading && <Loading />}
        {enginesError && <ErrorBox error={enginesError} />}
        {engines?.length === 0 && (
          <EmptyState title={t("No processing engines available")}>{t("Install the full image or connect an upscale worker to add one.")}</EmptyState>
        )}
        {engines && engines.length > 0 && (
          <Table>
            <thead>
              <tr>
                <Th>{t("Engine")}</Th>
                <Th>{t("Priority")}</Th>
                <Th>{t("Enabled")}</Th>
              </tr>
            </thead>
            <tbody>
              {engines.map((engine) => <EngineRow key={engine.id} engine={engine} onUpdate={updateEngine} />)}
            </tbody>
          </Table>
        )}
      </Card>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && (
        <EmptyState title={t("No workers yet")}>{t("A worker is the same mangarr image started with") + " "}<code>MANGARR_MODE=worker</code>{t(", a server address and a key from here. It dials in and asks for work, so it needs no port of its own.")}</EmptyState>
      )}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t("Worker")}</Th>
              <Th>{t("Roles")}</Th>
              <Th>{t("Doing now")}</Th>
              <Th>{t("Last 24 hours")}</Th>
              <Th>{t("Lifetime")}</Th>
              <Th>{t("Enabled")}</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((w) => (
              <tr key={w.id} className={w.enabled ? undefined : "opacity-60"}>
                <Td>
                  <div className="flex items-center gap-2">
                    <span className={`size-2 rounded-full ${w.online ? "bg-ok" : "bg-border"}`} title={w.online ? tr("online") : tr("offline")} />
                    <span className="font-medium">{w.name}</span>
                  </div>
                  <div className="font-mono text-xs text-muted">
                    {w.prefix}… {w.version && `· ${w.version}`} {w.platform && `· ${w.platform}`} {w.lastIp && `· ${w.lastIp}`}
                  </div>
                </Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {roles.map((r) => {
                      const on = w.roles.includes(r.key);
                      return (
                        <button
                          key={r.key}
                          title={r.help}
                          onClick={() => update(w, { roles: on ? w.roles.filter((x) => x !== r.key) : [...w.roles, r.key] })}
                          className={`rounded border px-2 py-0.5 text-xs ${on ? "border-accent bg-accent/15 text-fg" : "border-border text-muted hover:text-fg"}`}
                        >
                          {r.label}
                        </button>
                      );
                    })}
                  </div>
                </Td>
                <Td className="text-muted">
                  {w.busy?.length ? (
                    <div className="flex flex-col gap-1">
                      {w.busy.map((b) => (
                        <div key={b.taskId}>
                          <div className="text-xs text-fg">
                            {b.kind} · {b.series || "?"} {b.chapter && `ch. ${b.chapter}`}
                          </div>
                          <div className="flex items-center gap-2">
                            <div className="h-1 w-24 rounded bg-border">
                              <div className="h-1 rounded bg-accent" style={{ width: `${b.pagesTotal ? (100 * b.pagesDone) / b.pagesTotal : 0}%` }} />
                            </div>
                            <span className="text-xs">
                              {b.pagesDone}/{b.pagesTotal || "?"} · {bytes(b.bytesIn)}
                            </span>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <span className="text-xs">{t("idle · seen") + " "}{w.lastSeenAt ? relative(w.lastSeenAt) : tr("never")}</span>
                  )}
                </Td>
                <Td className="text-muted">
                  <div className="text-xs">
                    {w.recent.tasks}{" " + t("tasks")}{w.recent.failed > 0 && <span className="text-err"> · {w.recent.failed}{" " + t("failed")}</span>}
                  </div>
                  <div className="text-xs">
                    {w.recent.pages}{" " + t("pages ·") + " "}{bytes(w.recent.bytesIn)}{" " + t("in ·") + " "}{bytes(w.recent.bytesOut)}{" " + t("out")}</div>
                  {w.recent.seconds > 0 && (
                    <div className="text-xs">
                      {((w.recent.bytesIn / w.recent.seconds) / (1 << 20)).toFixed(1)}{" " + t("MB/s while busy")}</div>
                  )}
                </Td>
                <Td className="text-muted">
                  <div className="text-xs">
                    {w.tasksDone}{" " + t("tasks")}{w.tasksFailed > 0 && <span className="text-err"> · {w.tasksFailed}{" " + t("failed")}</span>}
                  </div>
                  <div className="text-xs">
                    {w.pagesDone}{" " + t("pages ·") + " "}{bytes(w.bytesIn)}{" " + t("in")}</div>
                </Td>
                <Td>
                  <Switch checked={w.enabled} onChange={(v) => update(w, { enabled: v })} />
                </Td>
                <Td className="text-right">
                  <IconButton title={t("Remove")} onClick={() => setRemoving(w)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {adding && (
        <AddWorker
          onClose={() => setAdding(false)}
          onCreated={(name, key) => {
            setAdding(false);
            setIssued({ name, key });
            reload();
          }}
        />
      )}
      {issued && <IssuedKey name={issued.name} value={issued.key} onClose={() => setIssued(null)} />}
      {removing && (
        <Modal
          open
          onClose={() => setRemoving(null)}
          title={`Remove ${removing.name}?`}
          footer={
            <>
              <Button onClick={() => setRemoving(null)}>{t("Cancel")}</Button>
              <Button
                variant="danger"
                onClick={async () => {
                  try {
                    await unwrap(api.DELETE("/api/v1/workers/{id}", { params: { path: { id: removing.id } } }));
                    reload();
                  } catch (e) {
                    toast.fromError(e);
                  }
                  setRemoving(null);
                }}
              >{t("Remove")}</Button>
            </>
          }
        >
          <p className="text-sm text-muted">{t("Its key stops working at once. Anything it is doing now is given to another worker or run here.")}</p>
        </Modal>
      )}
    </>
  );
}

function EngineRow({ engine, onUpdate }: { engine: ModuleResource; onUpdate: (engine: ModuleResource, patch: { enabled?: boolean; priority?: number }) => Promise<void> }) {
  const [priority, setPriority] = useState(engine.priority);
  useEffect(() => setPriority(engine.priority), [engine.priority]);
  const savePriority = () => {
    if (priority !== engine.priority) void onUpdate(engine, { priority });
  };
  return (
    <tr className={engine.enabled ? undefined : "opacity-60"}>
      <Td>
        <div className="flex items-center gap-2">
          <span className="font-medium">{engine.name}</span>
          <Badge tone={engine.implementation === "local" ? "accent" : "info"}>
            {engine.implementation === "local" ? t("Built into this server") : t("Remote worker pool")}
          </Badge>
        </div>
        {engine.error && <div className="mt-1 text-xs text-err">{engine.error}</div>}
      </Td>
      <Td>
        <Input
          className="w-24"
          type="number"
          value={priority}
          onChange={(e) => setPriority(Number(e.target.value))}
          onBlur={savePriority}
          onKeyDown={(e) => { if (e.key === "Enter") e.currentTarget.blur(); }}
          aria-label={t("Priority")}
          title={t("Lower first")}
        />
      </Td>
      <Td><Switch checked={engine.enabled} onChange={(enabled) => onUpdate(engine, { enabled })} /></Td>
    </tr>
  );
}

function AddWorker({ onClose, onCreated }: { onClose: () => void; onCreated: (name: string, key: string) => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<string[]>(["upscale"]);
  const [busy, setBusy] = useState(false);
  const create = async () => {
    setBusy(true);
    try {
      const r = await unwrap(api.POST("/api/v1/workers", { body: { name, roles: picked } }));
      onCreated(r.worker.name, r.key);
    } catch (e) {
      toast.fromError(e, tr("Could not add the worker"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={t("Add worker")}
      footer={
        <>
          <Button onClick={onClose}>{t("Cancel")}</Button>
          <Button variant="primary" disabled={!name.trim() || !picked.length || busy} onClick={create}>{t("Create key")}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-sm font-medium">{t("Name")}</label>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="gpu-box" />
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">{t("What it may do")}</span>
          {roles.map((r) => (
            <label key={r.key} className="flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-1"
                checked={picked.includes(r.key)}
                onChange={(e) => setPicked(e.target.checked ? [...picked, r.key] : picked.filter((x) => x !== r.key))}
              />
              <span>
                {r.label}
                <span className="block text-xs text-muted">{r.help}</span>
              </span>
            </label>
          ))}
        </div>
      </div>
    </Modal>
  );
}

function IssuedKey({ name, value, onClose }: { name: string; value: string; onClose: () => void }) {
  const toast = useToast();
  return (
    <Modal open onClose={onClose} title={`Key for ${name}`} footer={<Button variant="primary" onClick={onClose}>{t("Done")}</Button>}>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">{t("Copy it now: only its hash is kept here, so this is the one time it can be read.")}</p>
        <div className="flex items-center gap-2">
          <code className="flex-1 rounded border border-border bg-bg px-2 py-1 font-mono text-xs break-all">{value}</code>
          <IconButton
            title={t("Copy")}
            onClick={async () => {
              await navigator.clipboard.writeText(value);
              toast.success(tr("Key copied"));
            }}
          >
            <Copy className="size-4" />
          </IconButton>
        </div>
        <p className="text-xs text-muted">{t("Give it to the worker as") + " "}<code>MANGARR_WORKER_KEY</code>{t(", with") + " "}<code>MANGARR_SERVER_URL</code>{" " + t("pointing at this server.")}</p>
      </div>
    </Modal>
  );
}
