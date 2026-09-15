import { useAuthStatus } from "../api/queries";

/** Permission keys (internal/access). */
export type Perm = "admin" | "library.manage" | "requests.manage" | "requests.create" | "apps" | "download";

/** useAccount is the signed-in account and what it may do. */
export function useAccount() {
  const { data } = useAuthStatus();
  const account = data?.account;
  const perms = new Set(account?.permissions ?? []);
  const can = (p: Perm) => perms.has("admin") || perms.has(p);
  return { account, can, isAdmin: can("admin"), name: account?.displayName || account?.username || "" };
}
