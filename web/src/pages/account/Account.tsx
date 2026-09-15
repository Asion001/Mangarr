import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, KeyRound, LogOut, Monitor, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type Implementation, type ModuleResource } from "../../api/client";
import { defaultsOf } from "../../components/DynamicForm";
import { Badge, Button, Card, Confirm, ErrorBox, Field, IconButton, Input, Modal, PageHeader, Tabs } from "../../components/ui";
import { ModuleEditor, type Draft } from "../settings/Modules";
import { appAddress, DevicesCard, Guide, type App } from "../settings/ReadingApps";
import { useAccount } from "../../lib/account";
import { relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

const permLabel: Record<string, string> = {
  admin: "Administrator",
  "library.manage": "Manage the library",
  "requests.manage": "Handle requests",
  "requests.create": "Request series",
  apps: "Reading apps",
  download: "Download files",
};

/** browser names a session's device from its user agent. */
function browser(ua: string) {
  const b = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /iPad/.test(ua) ? "iPad" : /iPhone/.test(ua) ? "iPhone" : /Android/.test(ua) ? "Android" : /Mac OS X/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return os ? `${b} on ${os}` : b;
}

export function AccountPage() {
  const { account, name, can } = useAccount();
  return (
    <>
      <PageHeader title="My account" subtitle={
          account?.kind === "user" ? `Signed in as ${account.username}` : account?.kind === "anonymous" ? "Logins are turned off (MANGARR_AUTH_DISABLED)" : "Signed in with the API key"
        } />
      <div className="flex flex-col gap-6">
        <Card title={name}>
          <div className="flex flex-col gap-2 text-sm">
            <div>
              Group <Badge tone="info">{account?.group || "—"}</Badge>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-muted">You can:</span> read the library
              {(account?.permissions ?? []).map((p) => (
                <Badge key={p}>{permLabel[p] ?? p}</Badge>
              ))}
            </div>
          </div>
        </Card>
        {account?.kind === "user" && <NotificationsCard />}
        {can("apps") && <ReadingAppsCard />}
        {account?.kind === "user" && <PasswordCard />}
        {account?.kind === "user" && <SessionsCard />}
      </div>
    </>
  );
}

function ReadingAppsCard() {
  const { data: st } = useQuery({ queryKey: ["reading", "status"], queryFn: () => unwrap(api.GET("/api/v1/reading/status")) });
  const [app, setApp] = useState<App>("mihon");
  if (!st) return null;
  if (!st.enabled) {
    return (
      <Card title="Reading apps">
        <p className="text-sm text-muted">Mihon, KMReader and Paperback can read this library once an administrator turns on reading apps.</p>
      </Card>
    );
  }
  return (
    <>
      <Card title="Reading apps">
        <p className="mb-3 text-sm text-muted">Read in Mihon, KMReader or Paperback: they connect as your account and sync your progress.</p>
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
          <Guide app={app} address={appAddress(st.publicUrl, st.address)} />
        </div>
      </Card>
      <DevicesCard />
    </>
  );
}

function PasswordCard() {
  const toast = useToast();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password !== confirm) {
      setError(new Error("The new passwords don't match"));
      return;
    }
    setSaving(true);
    try {
      await unwrap(api.POST("/api/v1/auth/password", { body: { current, password } }));
      toast.success("Password changed", "Your other sessions were signed out");
      setCurrent("");
      setPassword("");
      setConfirm("");
    } catch (e) {
      setError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Card title="Password">
      <form onSubmit={submit} className="grid max-w-xl gap-4 sm:grid-cols-2">
        <Field label="Current password" className="sm:col-span-2">
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
        <Field label="New password" help="At least 8 characters.">
          <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </Field>
        <Field label="Confirm new password">
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
        </Field>
        {error !== null && (
          <div className="sm:col-span-2">
            <ErrorBox error={error} />
          </div>
        )}
        <div>
          <Button type="submit" icon={<KeyRound className="size-4" />} loading={saving}>
            Change password
          </Button>
        </div>
      </form>
    </Card>
  );
}

function SessionsCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data, error } = useQuery({ queryKey: ["me", "sessions"], queryFn: () => unwrap(api.GET("/api/v1/me/sessions")) });
  const revoke = async (id: string) => {
    try {
      await unwrap(api.DELETE("/api/v1/me/sessions/{id}", { params: { path: { id } } }));
      qc.invalidateQueries({ queryKey: ["me", "sessions"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const others = async () => {
    try {
      await unwrap(api.POST("/api/v1/me/sessions/revoke-others"));
      qc.invalidateQueries({ queryKey: ["me", "sessions"] });
      toast.success("Signed out everywhere else");
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Card
      title="Where you're signed in"
      actions={
        (data?.length ?? 0) > 1 && (
          <Button size="sm" icon={<LogOut className="size-3.5" />} onClick={others}>
            Sign out everywhere else
          </Button>
        )
      }
    >
      {error && <ErrorBox error={error} />}
      <div className="flex flex-col gap-2">
        {data?.map((s) => (
          <div key={s.id} className="flex items-center gap-3 rounded bg-panel-2 px-3 py-2 text-sm">
            <Monitor className="size-4 shrink-0 text-muted" />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="font-medium">
                {browser(s.userAgent)} {s.current && <Badge tone="ok">this browser</Badge>}
              </span>
              <span className="truncate text-xs text-muted">
                {s.ip || "unknown address"} · active {relative(s.lastSeenAt)} · signed in {relative(s.createdAt)}
              </span>
            </div>
            {!s.current && (
              <IconButton title="Sign out" onClick={() => revoke(s.id)}>
                <LogOut className="size-4" />
              </IconButton>
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}

/** NotificationsCard: your own notification targets (new chapters of series you follow, your requests). */
function NotificationsCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: targets, error } = useQuery({ queryKey: ["me-notifications"], queryFn: () => unwrap(api.GET("/api/v1/me/notifications")) });
  const { data: impls } = useQuery({
    queryKey: ["me-notifications", "schema"],
    queryFn: () => unwrap(api.GET("/api/v1/me/notifications/schema")),
    staleTime: Infinity,
  });
  const [picking, setPicking] = useState(false);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [deleting, setDeleting] = useState<ModuleResource | null>(null);
  const implOf = (name: string) => impls?.find((i) => i.name === name);
  const startNew = (impl: Implementation) => {
    setPicking(false);
    setDraft({ implementation: impl.name, name: impl.displayName, enabled: true, priority: 25, events: impl.events ?? [], settings: defaultsOf(impl.fields) });
  };
  const remove = async () => {
    if (!deleting) return;
    try {
      await unwrap(api.DELETE("/api/v1/me/notifications/{id}", { params: { path: { id: deleting.id } } }));
      qc.invalidateQueries({ queryKey: ["me-notifications"] });
      setDeleting(null);
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Card
      title="Notifications"
      actions={
        <Button size="sm" icon={<Plus className="size-4" />} onClick={() => setPicking(true)}>
          Add
        </Button>
      }
    >
      <p className="mb-3 text-sm text-muted">
        Get new chapters of the series you follow, and news about your requests, on your phone or chat. Follow a series with the bell on its page.
      </p>
      {error && <ErrorBox error={error} />}
      {targets?.length === 0 && <p className="text-sm text-muted">No notifications set up yet.</p>}
      <div className="flex flex-col gap-2">
        {targets?.map((m) => (
          <div key={m.id} className="flex items-center gap-3 rounded-md bg-panel-2 px-3 py-2 text-sm">
            <Bell className="size-4 text-muted" />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{m.name}</div>
              <div className="text-xs text-muted">
                {implOf(m.implementation)?.displayName ?? m.implementation}
                {(m.events ?? []).length > 0 && ` · ${m.events.length === 1 ? (m.events[0] === "chapter.imported" ? "new chapters only" : "requests only") : "everything"}`}
              </div>
              {m.error && <div className="text-xs text-err">{m.error}</div>}
            </div>
            {!m.enabled && <Badge>off</Badge>}
            <IconButton
              title="Edit"
              onClick={() =>
                setDraft({ id: m.id, implementation: m.implementation, name: m.name, enabled: m.enabled, priority: m.priority, events: m.events ?? [], settings: { ...m.settings } })
              }
            >
              <Pencil className="size-4" />
            </IconButton>
            <IconButton title="Delete" onClick={() => setDeleting(m)}>
              <Trash2 className="size-4" />
            </IconButton>
          </div>
        ))}
      </div>
      <Modal open={picking} onClose={() => setPicking(false)} title="Send my notifications to">
        <div className="flex flex-col gap-2">
          {impls?.map((i) => (
            <button key={i.name} onClick={() => startNew(i)} className="rounded-lg border border-border p-3 text-left hover:border-accent hover:bg-panel-2">
              <div className="font-medium">{i.displayName}</div>
              <div className="mt-0.5 text-sm text-muted">{i.description}</div>
            </button>
          ))}
        </div>
      </Modal>
      {draft && <ModuleEditor kind="notify" personal draft={draft} impl={implOf(draft.implementation)} onClose={() => setDraft(null)} />}
      <Confirm open={!!deleting} title="Delete notification" danger confirmLabel="Delete" message={`Delete ${deleting?.name}?`} onConfirm={remove} onClose={() => setDeleting(null)} />
    </Card>
  );
}
