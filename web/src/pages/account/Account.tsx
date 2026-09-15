import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, LogOut, Monitor } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { Badge, Button, Card, ErrorBox, Field, IconButton, Input, PageHeader } from "../../components/ui";
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
  const { account, name } = useAccount();
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
        {account?.kind === "user" && <PasswordCard />}
        {account?.kind === "user" && <SessionsCard />}
      </div>
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
