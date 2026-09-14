import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { Button, ErrorBox, Field, Input } from "../../components/ui";

export function LoginPage({ setup }: { setup: boolean }) {
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (setup && password !== confirm) {
      setError(new Error("Passwords do not match"));
      return;
    }
    setLoading(true);
    try {
      const body = { username, password };
      await unwrap(setup ? api.POST("/api/v1/auth/setup", { body }) : api.POST("/api/v1/auth/login", { body }));
      await qc.invalidateQueries();
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center p-4">
      <form onSubmit={submit} className="w-full max-w-sm rounded-xl border border-border bg-panel p-6 shadow-xl">
        <div className="mb-6 flex items-center gap-3">
          <img src="./favicon.svg" className="size-10" alt="" />
          <div>
            <h1 className="text-lg font-semibold">mangarr</h1>
            <p className="text-sm text-muted">{setup ? "Create the administrator account" : "Sign in to continue"}</p>
          </div>
        </div>
        <div className="flex flex-col gap-4">
          <Field label="Username">
            <Input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Password">
            <Input type="password" autoComplete={setup ? "new-password" : "current-password"} value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          {setup && (
            <Field label="Confirm password" help="At least 6 characters.">
              <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
            </Field>
          )}
          {error !== null && <ErrorBox error={error} />}
          <Button variant="primary" type="submit" loading={loading}>
            {setup ? "Create account" : "Sign in"}
          </Button>
        </div>
      </form>
    </div>
  );
}
