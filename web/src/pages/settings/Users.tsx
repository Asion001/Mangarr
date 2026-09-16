import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Link2, LogOut, Pencil, Plus, Trash2, UserPlus } from "lucide-react";
import clsx from "clsx";
import { api, basePath, unwrap, type S } from "../../api/client";
import { useReaders, useRootFolders, useTags } from "../../api/queries";
import { Badge, Button, Card, Confirm, EmptyState, ErrorBox, Field, IconButton, Input, Loading, Modal, PageHeader, Select, Switch, Table, Tabs, Td, Th } from "../../components/ui";
import { useAccount } from "../../lib/account";
import { dateTime, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useListParam } from "../../lib/urlState";

type User = S["UserView"];
type Group = S["GroupView"];
type Invite = S["InviteView"];
type Tab = "users" | "groups" | "invites";

const useUsers = () => useQuery({ queryKey: ["users"], queryFn: () => unwrap(api.GET("/api/v1/users")) });
const useGroups = () => useQuery({ queryKey: ["users", "groups"], queryFn: () => unwrap(api.GET("/api/v1/groups")) });
const usePermissions = () => useQuery({ queryKey: ["permissions"], queryFn: () => unwrap(api.GET("/api/v1/permissions")), staleTime: Infinity });

export function UsersPage() {
  const [tabParam, setTab] = useListParam("tab", "users");
  const tab = tabParam as Tab;
  return (
    <>
      <PageHeader title="Users & groups" subtitle="Share the library: everyone reads the same series, with their own progress. Groups decide what members can do and see." />
      <Tabs
        tabs={[
          { value: "users", label: "Users" },
          { value: "groups", label: "Groups" },
          { value: "invites", label: "Invite links" },
        ]}
        value={tab}
        onChange={setTab}
      />
      <div className="mt-4">
        {tab === "users" && <UsersTab />}
        {tab === "groups" && <GroupsTab />}
        {tab === "invites" && <InvitesTab />}
      </div>
    </>
  );
}

function UsersTab() {
  const qc = useQueryClient();
  const toast = useToast();
  const { account } = useAccount();
  const { data, isLoading, error } = useUsers();
  const [editing, setEditing] = useState<User | null>(null);
  const [creating, setCreating] = useState(false);
  const [password, setPassword] = useState<User | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["users"] });
  const signout = async (u: User) => {
    try {
      await unwrap(api.POST("/api/v1/users/{id}/signout", { params: { path: { id: u.id } } }));
      toast.success(`${u.username} was signed out`);
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  };
  const remove = async () => {
    if (!deleting) return;
    try {
      await unwrap(api.DELETE("/api/v1/users/{id}", { params: { path: { id: deleting.id } } }));
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
    setDeleting(null);
  };
  return (
    <>
      <div className="mb-3 flex justify-end">
        <Button variant="primary" icon={<UserPlus className="size-4" />} onClick={() => setCreating(true)}>
          Add user
        </Button>
      </div>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && (
        <div className="overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <Th>User</Th>
                <Th>Group</Th>
                <Th>Progress</Th>
                <Th>Last sign-in</Th>
                <Th>Signed in</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {data.map((u) => (
                <tr key={u.id} className={clsx(u.disabled && "opacity-60")}>
                  <Td>
                    <div className="font-medium">{u.displayName || u.username}</div>
                    {u.displayName && <div className="text-xs text-muted">{u.username}</div>}
                    {u.disabled && <Badge tone="warn">disabled</Badge>}
                  </Td>
                  <Td>
                    <Badge tone="info">{u.group}</Badge>
                  </Td>
                  <Td className="text-muted">{u.readerName || "—"}</Td>
                  <Td className="text-muted">{u.lastLoginAt ? relative(u.lastLoginAt) : "never"}</Td>
                  <Td className="text-muted">
                    {u.sessions} browser{u.sessions === 1 ? "" : "s"}, {u.devices} app{u.devices === 1 ? "" : "s"}
                  </Td>
                  <Td>
                    <div className="flex justify-end">
                      <IconButton title="Edit" onClick={() => setEditing(u)}>
                        <Pencil className="size-4" />
                      </IconButton>
                      <IconButton title="Set password" onClick={() => setPassword(u)}>
                        <KeyRound className="size-4" />
                      </IconButton>
                      <IconButton title="Sign out everywhere" onClick={() => signout(u)}>
                        <LogOut className="size-4" />
                      </IconButton>
                      {u.id !== account?.id && (
                        <IconButton title="Delete" onClick={() => setDeleting(u)}>
                          <Trash2 className="size-4" />
                        </IconButton>
                      )}
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      {creating && <UserModal onClose={() => setCreating(false)} />}
      {editing && <UserModal user={editing} onClose={() => setEditing(null)} />}
      {password && <PasswordModal user={password} onClose={() => setPassword(null)} />}
      <Confirm
        open={!!deleting}
        title="Delete user"
        danger
        confirmLabel="Delete"
        message={`Delete ${deleting?.username}? Their reader and its progress stay (Settings → Readers).`}
        onConfirm={remove}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}

function UserModal({ user, onClose }: { user?: User; onClose: () => void }) {
  const qc = useQueryClient();
  const { data: groups } = useGroups();
  const { data: readers } = useReaders();
  const { account } = useAccount();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [groupId, setGroupId] = useState(user?.groupId ?? 0);
  const [readerId, setReaderId] = useState(user?.readerId ?? 0);
  const [disabled, setDisabled] = useState(user?.disabled ?? false);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      if (user) {
        await unwrap(api.PUT("/api/v1/users/{id}", { params: { path: { id: user.id } }, body: { displayName, groupId, readerId, disabled } }));
      } else {
        await unwrap(api.POST("/api/v1/users", { body: { username, password, displayName: displayName || undefined, groupId: groupId || undefined } }));
      }
      qc.invalidateQueries({ queryKey: ["users"] });
      qc.invalidateQueries({ queryKey: ["readers"] });
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
      title={user ? `Edit ${user.username}` : "Add a user"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={saving} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {!user && (
          <>
            <Field label="Username">
              <Input autoFocus autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
            </Field>
            <Field label="Password" help="At least 8 characters. They can change it under My account.">
              <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
          </>
        )}
        <Field label="Name to show" help="Optional.">
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <Field label="Group">
          <Select value={groupId} onChange={(e) => setGroupId(Number(e.target.value))}>
            {!user && <option value={0}>Users</option>}
            {groups
              ?.filter((g) => user || g.builtin !== "users")
              .map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
          </Select>
        </Field>
        {user && (
          <>
            <Field label="Progress" help="The reader whose progress this user reads and writes (e.g. one you set up for their Komga account before).">
              <Select value={readerId} onChange={(e) => setReaderId(Number(e.target.value))}>
                {readers?.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name}
                  </option>
                ))}
              </Select>
            </Field>
            {user.id !== account?.id && <Switch checked={disabled} onChange={setDisabled} label="Disabled (can't sign in)" />}
          </>
        )}
        {error !== null && <ErrorBox error={error} />}
      </div>
    </Modal>
  );
}

function PasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const toast = useToast();
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const save = async () => {
    try {
      await unwrap(api.POST("/api/v1/users/{id}/password", { params: { path: { id: user.id } }, body: { password } }));
      toast.success(`Password set for ${user.username}`, "They were signed out everywhere");
      onClose();
    } catch (e) {
      setError(e);
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={`Set ${user.username}'s password`}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={password.length < 8} onClick={save}>
            Set password
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="New password" help="At least 8 characters.">
          <Input type="password" autoComplete="new-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {error !== null && <ErrorBox error={error} />}
      </div>
    </Modal>
  );
}

function GroupsTab() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data, isLoading, error } = useGroups();
  const { data: perms } = usePermissions();
  const { data: tags } = useTags();
  const { data: roots } = useRootFolders();
  const [editing, setEditing] = useState<Group | "new" | null>(null);
  const [deleting, setDeleting] = useState<Group | null>(null);
  const tagName = (id: number) => tags?.find((t) => t.id === id)?.label ?? `#${id}`;
  const rootName = (id: number) => roots?.find((r) => r.id === id)?.path ?? `#${id}`;
  const permName = (k: string) => perms?.find((p) => p.key === k)?.label ?? k;
  const remove = async () => {
    if (!deleting) return;
    try {
      await unwrap(api.DELETE("/api/v1/groups/{id}", { params: { path: { id: deleting.id } } }));
      qc.invalidateQueries({ queryKey: ["users"] });
    } catch (e) {
      toast.fromError(e);
    }
    setDeleting(null);
  };
  return (
    <>
      <div className="mb-3 flex justify-end">
        <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
          Add group
        </Button>
      </div>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      <div className="grid gap-3 md:grid-cols-2">
        {data?.map((g) => {
          const limited = g.includeTags.length > 0 || g.excludeTags.length > 0 || g.rootFolders.length > 0;
          return (
            <Card
              key={g.id}
              title={
                <span className="flex items-center gap-2">
                  {g.name} {g.builtin && <Badge>built-in</Badge>} <span className="text-xs font-normal text-muted">{g.members} member{g.members === 1 ? "" : "s"}</span>
                </span>
              }
              actions={
                <div className="flex">
                  <IconButton title="Edit" onClick={() => setEditing(g)}>
                    <Pencil className="size-4" />
                  </IconButton>
                  {!g.builtin && (
                    <IconButton title="Delete" onClick={() => setDeleting(g)}>
                      <Trash2 className="size-4" />
                    </IconButton>
                  )}
                </div>
              }
            >
              <div className="flex flex-col gap-2 text-sm">
                <div className="flex flex-wrap gap-1.5">
                  {g.permissions.length === 0 && <span className="text-muted">Read only</span>}
                  {g.permissions.map((p) => (
                    <Badge key={p} tone={p === "admin" ? "accent" : "default"}>
                      {permName(p)}
                    </Badge>
                  ))}
                </div>
                <div className="text-xs text-muted">
                  {!limited
                    ? "Sees every series"
                    : [
                        g.includeTags.length > 0 && `only tagged ${g.includeTags.map(tagName).join(" or ")}`,
                        g.excludeTags.length > 0 && `never tagged ${g.excludeTags.map(tagName).join(", ")}`,
                        g.rootFolders.length > 0 && `only in ${g.rootFolders.map(rootName).join(", ")}`,
                      ]
                        .filter(Boolean)
                        .join("; ")}
                  {g.autoApproveRequests && " · requests are added without approval"}
                </div>
              </div>
            </Card>
          );
        })}
      </div>
      {editing && <GroupModal group={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
      <Confirm
        open={!!deleting}
        title="Delete group"
        danger
        confirmLabel="Delete"
        message={`Delete ${deleting?.name}? Its members move to Users.`}
        onConfirm={remove}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}

function Chips<T extends number | string>({ options, value, onChange }: { options: { value: T; label: string }[]; value: T[]; onChange: (v: T[]) => void }) {
  if (options.length === 0) return <span className="text-sm text-muted">None yet</span>;
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((o) => {
        const on = value.includes(o.value);
        return (
          <button
            key={String(o.value)}
            type="button"
            onClick={() => onChange(on ? value.filter((v) => v !== o.value) : [...value, o.value])}
            className={clsx("rounded-full border px-2.5 py-0.5 text-xs", on ? "border-accent bg-accent/15 text-accent-2" : "border-border text-muted hover:text-fg")}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

function GroupModal({ group, onClose }: { group?: Group; onClose: () => void }) {
  const qc = useQueryClient();
  const { data: perms } = usePermissions();
  const { data: tags } = useTags();
  const { data: roots } = useRootFolders();
  const [name, setName] = useState(group?.name ?? "");
  const [permissions, setPermissions] = useState<string[]>(group?.permissions ?? ["requests.create", "apps", "download"]);
  const [includeTags, setInclude] = useState<number[]>(group?.includeTags ?? []);
  const [excludeTags, setExclude] = useState<number[]>(group?.excludeTags ?? []);
  const [rootFolders, setRoots] = useState<number[]>(group?.rootFolders ?? []);
  const [autoApprove, setAutoApprove] = useState(group?.autoApproveRequests ?? false);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const admins = group?.builtin === "admins";
  const tagOptions = (tags ?? []).map((t) => ({ value: t.id, label: t.label }));
  const save = async () => {
    setSaving(true);
    setError(null);
    const body = { name, permissions, includeTags, excludeTags, rootFolders, autoApproveRequests: autoApprove };
    try {
      if (group) await unwrap(api.PUT("/api/v1/groups/{id}", { params: { path: { id: group.id } }, body }));
      else await unwrap(api.POST("/api/v1/groups", { body }));
      qc.invalidateQueries({ queryKey: ["users"] });
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
      title={group ? `Edit ${group.name}` : "Add a group"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={saving} disabled={!name.trim()} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        {admins ? (
          <p className="text-sm text-muted">Admins can do everything and see every series.</p>
        ) : (
          <>
            <Field label="Members can" help="Everyone can read the series their group sees and keep their own progress.">
              <div className="flex flex-col gap-2">
                {perms?.map((p) => (
                  <label key={p.key} className="flex items-start gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={permissions.includes(p.key)}
                      onChange={(e) => setPermissions(e.target.checked ? [...permissions, p.key] : permissions.filter((x) => x !== p.key))}
                    />
                    <span>
                      <span className="font-medium">{p.label}</span> <span className="text-muted">— {p.description}</span>
                    </span>
                  </label>
                ))}
              </div>
            </Field>
            <Field label="Only series tagged" help="Members see series with any of these tags. None selected: every series.">
              <Chips options={tagOptions} value={includeTags} onChange={setInclude} />
            </Field>
            <Field label="Never series tagged" help="Hide series with these tags, e.g. an nsfw tag.">
              <Chips options={tagOptions} value={excludeTags} onChange={setExclude} />
            </Field>
            <Field label="Only these root folders" help="None selected: all root folders.">
              <Chips options={(roots ?? []).map((r) => ({ value: r.id, label: r.path }))} value={rootFolders} onChange={setRoots} />
            </Field>
            <Switch checked={autoApprove} onChange={setAutoApprove} label="Add members' requests without approval (when a source is found automatically)" />
          </>
        )}
        {error !== null && <ErrorBox error={error} />}
      </div>
    </Modal>
  );
}

function InvitesTab() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data, isLoading, error } = useQuery({ queryKey: ["users", "invites"], queryFn: () => unwrap(api.GET("/api/v1/invites")) });
  const [creating, setCreating] = useState(false);
  const remove = async (inv: Invite) => {
    try {
      await unwrap(api.DELETE("/api/v1/invites/{id}", { params: { path: { id: inv.id } } }));
      qc.invalidateQueries({ queryKey: ["users", "invites"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <>
      <div className="mb-3 flex items-center justify-between gap-3">
        <p className="text-sm text-muted">Send a friend a link: they choose their own username and password.</p>
        <Button variant="primary" icon={<Link2 className="size-4" />} onClick={() => setCreating(true)}>
          Create invite link
        </Button>
      </div>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <EmptyState title="No invite links yet" />}
      {!!data?.length && (
        <div className="overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <Th>For</Th>
                <Th>Group</Th>
                <Th>Used</Th>
                <Th>Expires</Th>
                <Th>Created</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {data.map((inv) => (
                <tr key={inv.id} className={clsx(!inv.active && "opacity-60")}>
                  <Td>{inv.note || <span className="text-muted">—</span>}</Td>
                  <Td>
                    <Badge tone="info">{inv.group}</Badge>
                  </Td>
                  <Td>
                    {inv.uses} / {inv.maxUses} {!inv.active && <Badge>done</Badge>}
                  </Td>
                  <Td className="text-muted">{inv.expiresAt ? dateTime(inv.expiresAt) : "never"}</Td>
                  <Td className="text-muted">{relative(inv.createdAt)}</Td>
                  <Td>
                    <IconButton title="Delete" onClick={() => remove(inv)}>
                      <Trash2 className="size-4" />
                    </IconButton>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      {creating && <InviteModal onClose={() => setCreating(false)} />}
    </>
  );
}

function InviteModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: groups } = useGroups();
  const [groupId, setGroupId] = useState(0);
  const [note, setNote] = useState("");
  const [maxUses, setMaxUses] = useState(1);
  const [expireDays, setExpireDays] = useState(7);
  const [link, setLink] = useState("");
  const [error, setError] = useState<unknown>(null);
  const create = async () => {
    try {
      const inv = await unwrap(api.POST("/api/v1/invites", { body: { groupId, note: note || undefined, maxUses, expireDays } }));
      setLink(`${window.location.origin}${basePath}/invite/${inv.token}`);
      qc.invalidateQueries({ queryKey: ["users", "invites"] });
    } catch (e) {
      setError(e);
    }
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      toast.success("Link copied");
    } catch {
      toast.error("Couldn't copy: select the link and copy it by hand");
    }
  };
  return (
    <Modal
      open
      onClose={onClose}
      title={link ? "Invite link" : "Create an invite link"}
      footer={
        link ? (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" onClick={create}>
              Create link
            </Button>
          </>
        )
      }
    >
      {link ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">Send this link to {note || "your friend"}. It's only shown now.</p>
          <div className="flex gap-2">
            <Input readOnly value={link} onFocus={(e) => e.currentTarget.select()} className="font-mono text-xs" />
            <Button icon={<Copy className="size-4" />} onClick={copy}>
              Copy
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <Field label="For" help="A note for you, e.g. their name. They see it on the invite page.">
            <Input autoFocus value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
          <Field label="Group">
            <Select value={groupId} onChange={(e) => setGroupId(Number(e.target.value))}>
              <option value={0}>Users</option>
              {groups
                ?.filter((g) => g.builtin !== "users")
                .map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
            </Select>
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Accounts it can create">
              <Input type="number" min={1} max={100} value={maxUses} onChange={(e) => setMaxUses(Number(e.target.value))} />
            </Field>
            <Field label="Expires after (days)" help="0 = never">
              <Input type="number" min={0} max={365} value={expireDays} onChange={(e) => setExpireDays(Number(e.target.value))} />
            </Field>
          </div>
          {error !== null && <ErrorBox error={error} />}
        </div>
      )}
    </Modal>
  );
}
