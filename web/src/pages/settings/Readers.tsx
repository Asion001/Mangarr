import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw, Trash2, UserPlus } from "lucide-react";
import { api, unwrap, type Reader } from "../../api/client";
import { useModules, usePushCommand, useReaders, useSchema } from "../../api/queries";
import { DynamicForm } from "../../components/DynamicForm";
import { Badge, Button, Card, Confirm, EmptyState, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, Select, Switch } from "../../components/ui";
import { relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

export function ReadersPage() {
  const { data, isLoading, error } = useReaders();
  const { data: libs } = useModules("library");
  const push = usePushCommand();
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [account, setAccount] = useState<Reader | null>(null);
  const [deleting, setDeleting] = useState<Reader | null>(null);
  const progressLibs = (libs ?? []).filter((l) => l.capabilities.includes("progress"));

  const add = async () => {
    try {
      await unwrap(api.POST("/api/v1/readers", { body: { name, countForCleanup: true } }));
      setName("");
      qc.invalidateQueries({ queryKey: ["readers"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const update = async (r: Reader, countForCleanup: boolean) => {
    await unwrap(api.PUT("/api/v1/readers/{id}", { params: { path: { id: r.id } }, body: { name: r.name, countForCleanup } }));
    qc.invalidateQueries({ queryKey: ["readers"] });
  };
  const removeAccount = async (r: Reader, accountId: number) => {
    await unwrap(api.DELETE("/api/v1/readers/{id}/accounts/{accountId}", { params: { path: { id: r.id, accountId } } }));
    qc.invalidateQueries({ queryKey: ["readers"] });
  };
  const remove = async () => {
    if (!deleting) return;
    await unwrap(api.DELETE("/api/v1/readers/{id}", { params: { path: { id: deleting.id } } }));
    qc.invalidateQueries({ queryKey: ["readers"] });
    setDeleting(null);
  };

  return (
    <>
      <PageHeader
        title="Readers"
        subtitle="People who read your library. Their progress (from Komga/Kavita) decides what read-based cleanup may delete."
        actions={
          <Button icon={<RefreshCw className="size-4" />} onClick={() => push.mutate({ name: "SyncReadProgress", label: "Syncing read progress" })}>
            Sync now
          </Button>
        }
      />
      {progressLibs.length === 0 && (
        <div className="mb-4">
          <ErrorBox error="Add a Komga or Kavita library server first (Settings → Library servers); readers link their own account there." />
        </div>
      )}
      <div className="mb-4 flex max-w-md gap-2">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Reader name" />
        <Button variant="primary" icon={<UserPlus className="size-4" />} disabled={!name} onClick={add}>
          Add reader
        </Button>
      </div>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <EmptyState title="No readers">Cleanup only deletes chapters that every reader counting for cleanup has finished.</EmptyState>}
      <div className="grid gap-3 md:grid-cols-2">
        {data?.map((r) => (
          <Card
            key={r.id}
            title={
              <span className="flex items-center gap-2">
                {r.name} <Badge>{r.completedCount} chapters read</Badge>
              </span>
            }
            actions={
              <IconButton title="Delete reader" onClick={() => setDeleting(r)}>
                <Trash2 className="size-4" />
              </IconButton>
            }
          >
            <div className="flex flex-col gap-3">
              <Switch checked={r.countForCleanup} onChange={(v) => update(r, v)} label="Counts for cleanup" />
              {r.accounts.map((a) => (
                <div key={a.id} className="flex items-center gap-2 rounded bg-panel-2 px-3 py-2 text-sm">
                  <Badge tone="info">{a.moduleName}</Badge>
                  <span className="flex-1 truncate">{a.externalUser}</span>
                  <span className="text-xs text-muted">synced {relative(a.lastSyncAt)}</span>
                  {a.lastError && <Badge tone="err" title={a.lastError}>error</Badge>}
                  <IconButton title="Remove account" onClick={() => removeAccount(r, a.id)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </div>
              ))}
              {progressLibs.length > 0 && (
                <div>
                  <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setAccount(r)}>
                    Link account
                  </Button>
                </div>
              )}
            </div>
          </Card>
        ))}
      </div>
      {account && <AccountModal reader={account} onClose={() => setAccount(null)} />}
      <Confirm open={!!deleting} title="Delete reader" danger confirmLabel="Delete" message={`Delete ${deleting?.name} and their progress?`} onConfirm={remove} onClose={() => setDeleting(null)} />
    </>
  );
}

function AccountModal({ reader, onClose }: { reader: Reader; onClose: () => void }) {
  const { data: libs } = useModules("library");
  const { data: schema } = useSchema("library");
  const qc = useQueryClient();
  const toast = useToast();
  const progressLibs = (libs ?? []).filter((l) => l.capabilities.includes("progress"));
  const [moduleId, setModuleId] = useState(progressLibs[0]?.id ?? 0);
  const [creds, setCreds] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mod = progressLibs.find((l) => l.id === moduleId);
  const fields = schema?.find((i) => i.name === mod?.implementation)?.accountFields ?? [];
  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const credentials: Record<string, string> = {};
      for (const [k, v] of Object.entries(creds)) credentials[k] = String(v ?? "");
      await unwrap(api.POST("/api/v1/readers/{id}/accounts", { params: { path: { id: reader.id } }, body: { moduleId, credentials } }));
      qc.invalidateQueries({ queryKey: ["readers"] });
      toast.success("Account linked");
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={`Link an account for ${reader.name}`}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={saving} onClick={save}>
            Test & save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Library server">
          <Select value={moduleId} onChange={(e) => (setModuleId(Number(e.target.value)), setCreds({}))}>
            {progressLibs.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </Select>
        </Field>
        <DynamicForm fields={fields} values={creds} onChange={setCreds} />
        {error !== null && <ErrorBox error={error} />}
      </div>
    </Modal>
  );
}
