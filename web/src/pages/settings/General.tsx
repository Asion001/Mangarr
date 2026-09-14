import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { useTags } from "../../api/queries";
import { Badge, Button, Card, EnvLock, ErrorBox, Field, IconButton, Input, Loading, PageHeader } from "../../components/ui";
import { useToast } from "../../lib/toast";
import { useSettingsDoc } from "./useSettingsDoc";

type General = S["GeneralSettingsResource"];

export function GeneralPage() {
  const { value: g, patch, save, saving, isLoading, error, setValue, lock } = useSettingsDoc<General>("general");
  const toast = useToast();
  const regen = async () => {
    try {
      const v = await unwrap(api.POST("/api/v1/settings/general/apikey"));
      setValue(v);
      toast.success("New API key generated");
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <>
      <PageHeader
        title="General"
        actions={
          <Button variant="primary" loading={saving} onClick={() => save()}>
            Save
          </Button>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {g && (
        <Card title="Server" className="mb-6">
          <div className="grid gap-4 md:grid-cols-2">
            <Field env={lock("instanceName")} label="Instance name">
              <Input value={g.instanceName} onChange={(e) => patch({ instanceName: e.target.value })} />
            </Field>
            <Field env={lock("publicUrl")} label="Public URL" help="Used for links in notifications, e.g. https://mangarr.example.com">
              <Input value={g.publicUrl} onChange={(e) => patch({ publicUrl: e.target.value })} />
            </Field>
            <Field env={lock("backupRetention")} label="Keep scheduled backups">
              <Input type="number" min={1} value={g.backupRetention} onChange={(e) => patch({ backupRetention: Number(e.target.value) })} />
            </Field>
            <Field
              label={
                <>
                  API key <EnvLock env={lock("apiKey")} />
                </>
              }
              help="Send as X-Api-Key header. API docs: /api/docs"
            >
              <div className="flex gap-2">
                <Input readOnly value={g.apiKey} className="font-mono text-xs" />
                <IconButton title="Copy" onClick={() => (navigator.clipboard.writeText(g.apiKey), toast.info("Copied"))}>
                  <Copy className="size-4" />
                </IconButton>
                <Button onClick={regen} disabled={!!lock("apiKey")} icon={<KeyRound className="size-4" />}>
                  Regenerate
                </Button>
              </div>
            </Field>
          </div>
        </Card>
      )}
      <Tags />
      <Password />
    </>
  );
}

function Tags() {
  const { data } = useTags();
  const qc = useQueryClient();
  const toast = useToast();
  const [label, setLabel] = useState("");
  const add = async () => {
    try {
      await unwrap(api.POST("/api/v1/tags", { body: { label } }));
      setLabel("");
      qc.invalidateQueries({ queryKey: ["tags"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const remove = async (id: number) => {
    await unwrap(api.DELETE("/api/v1/tags/{id}", { params: { path: { id } } }));
    qc.invalidateQueries({ queryKey: ["tags"] });
  };
  return (
    <Card title="Tags" className="mb-6">
      <p className="mb-3 text-sm text-muted">Tags limit notifications to some series, and the “keep” tag excludes series from cleanup.</p>
      <div className="mb-3 flex flex-wrap gap-2">
        {data?.map((t) => (
          <Badge key={t.id}>
            {t.label}
            <button className="ml-1 hover:text-err" onClick={() => remove(t.id)}>
              <Trash2 className="size-3" />
            </button>
          </Badge>
        ))}
      </div>
      <div className="flex max-w-sm gap-2">
        <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="keep" />
        <Button icon={<Plus className="size-4" />} disabled={!label} onClick={add}>
          Add
        </Button>
      </div>
    </Card>
  );
}

function Password() {
  const toast = useToast();
  const [pw, setPw] = useState("");
  const [saving, setSaving] = useState(false);
  const change = async () => {
    setSaving(true);
    try {
      await unwrap(api.POST("/api/v1/auth/password", { body: { password: pw } }));
      setPw("");
      toast.success("Password changed");
    } catch (e) {
      toast.fromError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Card title="Password">
      <div className="flex max-w-sm gap-2">
        <Input type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} placeholder="New password" />
        <Button disabled={pw.length < 6} loading={saving} onClick={change}>
          Change
        </Button>
      </div>
    </Card>
  );
}
