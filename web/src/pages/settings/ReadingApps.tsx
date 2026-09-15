import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { Link } from "react-router";
import { api, unwrap, type S } from "../../api/client";
import { useReaders } from "../../api/queries";
import { Badge, Button, Card, Confirm, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, Select, Switch, Table, Tabs, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useSettingsDoc } from "./useSettingsDoc";

type ReadingSettings = S["Reading"];
type Key = S["ReadingKey"];
type App = "mihon" | "kmreader" | "paperback";

/** The address apps should use: the configured one, else this host on the API's port. */
function appAddress(publicUrl: string, listen?: string) {
  if (publicUrl) return publicUrl.replace(/\/+$/, "");
  const port = listen?.split(":").pop() || "25600";
  return `${window.location.protocol}//${window.location.hostname}:${port}`;
}

export function ReadingAppsPage() {
  const { value: r, patch, save, saving, isLoading, error, lock } = useSettingsDoc<ReadingSettings>("reading");
  const status = useQuery({ queryKey: ["reading", "status"], queryFn: () => unwrap(api.GET("/api/v1/reading/status")), refetchInterval: 10_000 });
  const { data: readers } = useReaders();
  const [app, setApp] = useState<App>("mihon");
  const st = status.data;
  const address = appAddress(r?.publicUrl ?? "", st?.address);
  const saveAndRefresh = async () => {
    await save();
    setTimeout(() => status.refetch(), 300);
  };
  return (
    <>
      <PageHeader
        title="Reading apps"
        subtitle="Read your whole mangarr library in Mihon, KMReader or Paperback through a Komga-compatible API, with progress synced both ways."
        actions={
          <Button variant="primary" loading={saving} onClick={saveAndRefresh}>
            Save
          </Button>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {r && (
        <div className="flex flex-col gap-6">
          <Card
            title={
              <span className="flex items-center gap-2">
                Komga-compatible API
                {st?.listening ? (
                  <Badge tone="ok">listening on {st.address}</Badge>
                ) : st?.error ? (
                  <Badge tone="err" title={st.error}>
                    can't listen
                  </Badge>
                ) : (
                  <Badge>off</Badge>
                )}
              </span>
            }
          >
            <div className="grid gap-5 md:grid-cols-2">
              <div className="flex flex-col gap-3">
                <Switch env={lock("enabled")} checked={r.enabled} onChange={(v) => patch({ enabled: v })} label={<b>Allow Komga apps to connect</b>} />
                <p className="text-xs text-muted">
                  Apps see every series and every chapter mangarr knows, downloaded or not. The API runs on its own port (like Komga's, 25600), so apps connect to it as they would to a Komga server.
                </p>
                {st?.error && <ErrorBox error={`The API can't listen: ${st.error}`} />}
                <Switch env={lock("downloadOnOpen")} checked={r.downloadOnOpen} onChange={(v) => patch({ downloadOnOpen: v })} label="Download chapters opened before they're downloaded" />
                <p className="-mt-2 text-xs text-muted">Chapters that aren't downloaded are streamed from the source meanwhile.</p>
                <Switch
                  env={lock("readAhead.enabled")}
                  checked={r.readAhead.enabled}
                  onChange={(v) => patch({ readAhead: { ...r.readAhead, enabled: v } })}
                  label="Download the next chapters while someone reads"
                />
              </div>
              <div className="flex flex-col gap-4">
                <Field env={lock("publicUrl")} label="Address apps should use" help={`Shown in the guides below. Empty: ${appAddress("", st?.address)}`}>
                  <Input value={r.publicUrl} placeholder="https://manga.example.com:25600" onChange={(e) => patch({ publicUrl: e.target.value })} />
                </Field>
                <Field env={lock("readerId")} label="Reader" help="Whose progress the apps read and write. Its sync health is under Settings → Readers.">
                  <Select value={r.readerId} onChange={(e) => patch({ readerId: Number(e.target.value) })}>
                    <option value={0}>First reader{readers?.[0] ? ` (${readers[0].name})` : ""}</option>
                    {readers?.map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.name}
                      </option>
                    ))}
                  </Select>
                </Field>
                <Field env={lock("readAhead.chapters")} label="Chapters to download ahead" help="After the furthest chapter a reader has started, in any app or library server.">
                  <Input
                    type="number"
                    min={1}
                    max={50}
                    value={r.readAhead.chapters}
                    disabled={!r.readAhead.enabled}
                    onChange={(e) => patch({ readAhead: { ...r.readAhead, chapters: Number(e.target.value) } })}
                  />
                </Field>
              </div>
            </div>
          </Card>

          <DevicesCard />

          <Card title="Connect an app">
            <Tabs
              tabs={[
                { value: "mihon", label: "Mihon (Android)" },
                { value: "kmreader", label: "KMReader (iPhone, iPad)" },
                { value: "paperback", label: "Paperback (iPhone, iPad)" },
              ]}
              value={app}
              onChange={setApp}
            />
            <div className="mt-4 text-sm">
              <Guide app={app} address={address} />
            </div>
            {!r.enabled && <p className="mt-4 text-sm text-warn">Turn on "Allow Komga apps to connect" first.</p>}
          </Card>
        </div>
      )}
    </>
  );
}

function Code({ children }: { children: string }) {
  return <code className="rounded bg-panel-2 px-1.5 py-0.5 text-xs break-all">{children}</code>;
}

function Guide({ app, address }: { app: App; address: string }) {
  switch (app) {
    case "mihon":
      return (
        <ol className="flex list-decimal flex-col gap-2 pl-5">
          <li>
            In Mihon, open <b>Browse → Extensions</b> and install the <b>Komga</b> extension (Keiyoushi repository).
          </li>
          <li>
            Open the extension's settings and set the address to <Code>{address}</Code>. For the login, add a device under Devices and paste its API key (or use your
            mangarr username and password).
          </li>
          <li>
            The Komga source now lists every mangarr series, downloaded or not. Chapters that aren't downloaded are streamed from the source.
          </li>
          <li>
            For progress sync, open <b>Settings → Tracking</b> and turn on <b>Komga</b> under enhanced services. Mihon then tells mangarr the chapters you finish, and picks up what you
            read elsewhere when it refreshes tracking. It syncs finished chapters, not pages.
          </li>
        </ol>
      );
    case "kmreader":
      return (
        <ol className="flex list-decimal flex-col gap-2 pl-5">
          <li>
            In KMReader, add a server with the address <Code>{address}</Code>.
          </li>
          <li>Sign in with an API key from Devices, or with your mangarr username and password (KMReader then creates its own device key).</li>
          <li>
            Progress syncs page by page, and changes made elsewhere show up live. Downloaded chapters can be saved for offline reading; the others are streamed.
          </li>
        </ol>
      );
    case "paperback":
      return (
        <ol className="flex list-decimal flex-col gap-2 pl-5">
          <li>
            In Paperback, add the <b>Komga</b> extension from the default extensions repository.
          </li>
          <li>
            Set the server address to <Code>{address}</Code>. Log in with any username and a device key from Devices as the password (or your mangarr
            username and password).
          </li>
          <li>Enable the Komga tracker in Paperback to send finished chapters to mangarr.</li>
        </ol>
      );
  }
}

function DevicesCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const keys = useQuery({ queryKey: ["reading", "keys"], queryFn: () => unwrap(api.GET("/api/v1/reading/keys")) });
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<Key | null>(null);
  const remove = async () => {
    if (!deleting) return;
    try {
      await unwrap(api.DELETE("/api/v1/reading/keys/{id}", { params: { path: { id: deleting.id } } }));
      qc.invalidateQueries({ queryKey: ["reading", "keys"] });
    } catch (e) {
      toast.fromError(e);
    }
    setDeleting(null);
  };
  return (
    <Card
      title="Devices"
      actions={
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setAdding(true)}>
          Add device
        </Button>
      }
    >
      <p className="mb-3 text-sm text-muted">
        Each app gets its own API key, so you can see which device synced what (Settings → <Link to="/settings/readers" className="text-accent-2 hover:underline">Readers</Link>) and revoke one without
        the others.
      </p>
      {keys.error && <ErrorBox error={keys.error} />}
      {keys.data?.length === 0 && <p className="text-sm text-muted">No devices yet.</p>}
      {!!keys.data?.length && (
        <div className="overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <Th>Device</Th>
                <Th>Key</Th>
                <Th>Last used</Th>
                <Th>Added</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {keys.data.map((k) => (
                <tr key={k.id}>
                  <Td className="font-medium">{k.comment || "Unnamed"}</Td>
                  <Td>
                    <Code>{`${k.prefix}…`}</Code>
                  </Td>
                  <Td className="text-muted">{k.lastUsedAt ? `${relative(k.lastUsedAt)}${k.lastClient ? ` · ${k.lastClient}` : ""}` : "never"}</Td>
                  <Td className="text-muted">{relative(k.createdAt)}</Td>
                  <Td>
                    <IconButton title="Revoke key" onClick={() => setDeleting(k)}>
                      <Trash2 className="size-4" />
                    </IconButton>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      {adding && <AddDeviceModal onClose={() => setAdding(false)} />}
      <Confirm
        open={!!deleting}
        title="Revoke key"
        danger
        confirmLabel="Revoke"
        message={`${deleting?.comment || "This device"} won't be able to connect with this key any more.`}
        onConfirm={remove}
        onClose={() => setDeleting(null)}
      />
    </Card>
  );
}

function AddDeviceModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [saving, setSaving] = useState(false);
  const create = async () => {
    setSaving(true);
    try {
      const k = await unwrap(api.POST("/api/v1/reading/keys", { body: { comment: name.trim() } }));
      setKey(k.key);
      qc.invalidateQueries({ queryKey: ["reading", "keys"] });
    } catch (e) {
      toast.fromError(e);
    } finally {
      setSaving(false);
    }
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(key);
      toast.success("Key copied");
    } catch {
      toast.error("Couldn't copy: select the key and copy it by hand");
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={key ? "Device added" : "Add a device"}
      footer={
        key ? (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" icon={<KeyRound className="size-4" />} loading={saving} disabled={!name.trim()} onClick={create}>
              Create key
            </Button>
          </>
        )
      }
    >
      {key ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">Enter this API key in the app. It's only shown now.</p>
          <div className="flex gap-2">
            <Input readOnly value={key} onFocus={(e) => e.currentTarget.select()} className="font-mono text-xs" />
            <Button icon={<Copy className="size-4" />} onClick={copy}>
              Copy
            </Button>
          </div>
        </div>
      ) : (
        <Field label="Device name" help="For example Mihon phone or KMReader iPad.">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && name.trim() && create()} />
        </Field>
      )}
    </Modal>
  );
}
