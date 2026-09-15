import type { ReactNode } from "react";
import { Lock } from "lucide-react";
import { useAccount, type Perm } from "../lib/account";
import { EmptyState } from "./ui";

/** Need shows children only to accounts with the permission (any of a list). */
export function Need({ perm, children }: { perm: Perm | Perm[]; children: ReactNode }) {
  const { can } = useAccount();
  if (can(perm)) return <>{children}</>;
  return (
    <EmptyState title="Not available for your account" icon={<Lock className="size-8" />}>
      Ask an administrator if you need this.
    </EmptyState>
  );
}
