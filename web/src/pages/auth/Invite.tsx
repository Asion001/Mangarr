import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { LogIn } from "lucide-react";
import { api, basePath, unwrap } from "../../api/client";
import { useAuthStatus } from "../../api/queries";
import { Button, ErrorBox, Field, Input, Loading } from "../../components/ui";

/** InvitePage lets someone with an invite link create their account. */
export function InvitePage({ token }: { token: string }) {
  const info = useQuery({
    queryKey: ["invite", token],
    queryFn: () => unwrap(api.GET("/api/v1/invites/redeem/{token}", { params: { path: { token } } })),
    retry: false,
  });
  const { data: status } = useAuthStatus();
  const sso = status?.sso;
  const passwords = status?.passwordLogin ?? true;
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password !== confirm) {
      setError(new Error("The passwords don't match"));
      return;
    }
    setLoading(true);
    try {
      await unwrap(api.POST("/api/v1/invites/redeem/{token}", { params: { path: { token } }, body: { username, password, displayName: displayName || undefined } }));
      window.location.replace(basePath + "/"); // signed in
    } catch (err) {
      setError(err);
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-xl border border-border bg-panel p-6 shadow-xl">
        <div className="mb-6 flex items-center gap-3">
          <img src="./favicon.svg" className="size-10" alt="" />
          <div>
            <h1 className="text-lg font-semibold">{info.data?.instance || "mangarr"}</h1>
            <p className="text-sm text-muted">You're invited to read here</p>
          </div>
        </div>
        {info.isLoading && <Loading />}
        {info.error && (
          <div className="flex flex-col gap-4">
            <ErrorBox error={info.error} />
            <a href={basePath + "/"} className="text-sm text-accent-2 hover:underline">
              Sign in instead
            </a>
          </div>
        )}
        {info.data && (
          <form onSubmit={submit} className="flex flex-col gap-4">
            {info.data.note && <p className="text-sm">{info.data.note}</p>}
            <p className="text-sm text-muted">Choose how you sign in. Your reading progress is your own.</p>
            {sso && (
              <>
                <a href={`${basePath}/api/v1/auth/oidc/login?invite=${encodeURIComponent(token)}`}>
                  <Button variant="primary" type="button" className="w-full" icon={<LogIn className="size-4" />}>
                    {sso.label}
                  </Button>
                </a>
                {passwords && (
                  <div className="flex items-center gap-3 text-xs text-muted">
                    <span className="h-px flex-1 bg-border" /> or make an account here <span className="h-px flex-1 bg-border" />
                  </div>
                )}
              </>
            )}
            {passwords && <>
            <Field label="Username">
              <Input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
            </Field>
            <Field label="Name to show" help="Optional; others see this instead of your username.">
              <Input autoComplete="nickname" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
            </Field>
            <Field label="Password" help="At least 8 characters.">
              <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
            </Field>
            <Field label="Confirm password">
              <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
            </Field>
            {error !== null && <ErrorBox error={error} />}
            <Button variant={sso ? "secondary" : "primary"} type="submit" loading={loading}>
              Create account
            </Button>
            </>}
          </form>
        )}
      </div>
    </div>
  );
}
