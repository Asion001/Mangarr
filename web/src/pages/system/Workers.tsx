import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { Button, EmptyState, ErrorBox, IconButton, Input, Loading, Modal, PageHeader, Switch, Table, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";
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
  const { data, isLoading, error } = useQuery({
    queryKey: ["workers"],
    queryFn: () => unwrap(api.GET("/api/v1/workers")),
    refetchInterval: 15000,
  });
  const [adding, setAdding] = useState(false);
  const [issued, setIssued] = useState<{ name: string; key: string } | null>(null);
  const [removing, setRemoving] = useState<Worker | null>(null);

  const reload = () => qc.invalidateQueries({ queryKey: ["workers"] });
  const update = async (w: Worker, body: { enabled?: boolean; roles?: string[] }) => {
    try {
      await unwrap(api.PUT("/api/v1/workers/{id}", { params: { path: { id: w.id } }, body }));
      reload();
    } catch (e) {
      toast.fromError(e, "Could not update the worker");
    }
  };

  return (
    <>
      <PageHeader
        title="Workers"
        subtitle="Machines that download, upscale and encode for this server"
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
            Add worker
          </Button>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && (
        <EmptyState title="No workers yet">
          A worker is the same mangarr image started with <code>MANGARR_MODE=worker</code>, a server address and a key from here. It dials in and asks
          for work, so it needs no port of its own.
        </EmptyState>
      )}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Worker</Th>
              <Th>Roles</Th>
              <Th>Seen</Th>
              <Th>Done</Th>
              <Th>Enabled</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((w) => (
              <tr key={w.id} className={w.enabled ? undefined : "opacity-60"}>
                <Td>
                  <div className="flex items-center gap-2">
                    <span className={`size-2 rounded-full ${w.online ? "bg-ok" : "bg-border"}`} title={w.online ? "online" : "offline"} />
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
                <Td className="text-muted">{w.lastSeenAt ? relative(w.lastSeenAt) : "never"}</Td>
                <Td className="text-muted">
                  {w.tasksDone} tasks
                  {w.tasksFailed > 0 && <span className="text-err"> · {w.tasksFailed} failed</span>}
                </Td>
                <Td>
                  <Switch checked={w.enabled} onChange={(v) => update(w, { enabled: v })} />
                </Td>
                <Td className="text-right">
                  <IconButton title="Remove" onClick={() => setRemoving(w)}>
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
              <Button onClick={() => setRemoving(null)}>Cancel</Button>
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
              >
                Remove
              </Button>
            </>
          }
        >
          <p className="text-sm text-muted">Its key stops working at once. Anything it is doing now is given to another worker or run here.</p>
        </Modal>
      )}
    </>
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
      toast.fromError(e, "Could not add the worker");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title="Add worker"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!name.trim() || !picked.length || busy} onClick={create}>
            Create key
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-sm font-medium">Name</label>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="gpu-box" />
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">What it may do</span>
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
    <Modal open onClose={onClose} title={`Key for ${name}`} footer={<Button variant="primary" onClick={onClose}>Done</Button>}>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">Copy it now: only its hash is kept here, so this is the one time it can be read.</p>
        <div className="flex items-center gap-2">
          <code className="flex-1 rounded border border-border bg-bg px-2 py-1 font-mono text-xs break-all">{value}</code>
          <IconButton
            title="Copy"
            onClick={async () => {
              await navigator.clipboard.writeText(value);
              toast.success("Key copied");
            }}
          >
            <Copy className="size-4" />
          </IconButton>
        </div>
        <p className="text-xs text-muted">
          Give it to the worker as <code>MANGARR_WORKER_KEY</code>, with <code>MANGARR_SERVER_URL</code> pointing at this server.
        </p>
      </div>
    </Modal>
  );
}
