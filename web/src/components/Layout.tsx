import { useState, type ReactNode } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import clsx from "clsx";
import { useQueryClient } from "@tanstack/react-query";
import {
  BookOpen,
  PlusCircle,
  Activity,
  AlertCircle,
  Compass,
  Settings,
  Server,
  Menu,
  X,
  LogOut,
  Eraser,
  Download,
  FileUp,
  UserRound,
  Inbox,
} from "lucide-react";
import { api } from "../api/client";
import { useHealth, usePendingRequests, useQueue } from "../api/queries";
import { useAccount, type Perm } from "../lib/account";

type NavItem = { to: string; label: string; icon: ReactNode; need?: Perm | Perm[]; children?: { to: string; label: string }[] };

export function Layout() {
  const [open, setOpen] = useState(false);
  const loc = useLocation();
  const { can, isAdmin, name } = useAccount();
  const { data: queue } = useQueue({ pageSize: 1 }, can("library.manage"));
  const queued = queue?.total ?? 0;
  const { data: health } = useHealth(isAdmin);
  const qc = useQueryClient();
  const issues = (health?.checks ?? []).filter((c) => c.type === "error" || c.type === "warning").length;
  const { data: requestCount } = usePendingRequests(can(["requests.manage", "library.manage"]));
  const pendingRequests = requestCount?.pending ?? 0;

  const nav: NavItem[] = [
    { to: "/", label: "Series", icon: <BookOpen className="size-4" /> },
    { to: "/add", label: "Add series", icon: <PlusCircle className="size-4" />, need: "library.manage" },
    { to: "/requests", label: "Requests", icon: <Inbox className="size-4" />, need: ["requests.create", "requests.manage", "library.manage"] },
    { to: "/import", label: "Import library", icon: <FileUp className="size-4" />, need: "admin" },
    {
      to: "/activity",
      label: "Activity",
      icon: <Download className="size-4" />,
      need: "library.manage",
      children: [
        { to: "/activity/queue", label: "Queue" },
        { to: "/activity/history", label: "History" },
        { to: "/activity/blocklist", label: "Blocklist" },
      ],
    },
    { to: "/wanted", label: "Wanted", icon: <AlertCircle className="size-4" />, need: "library.manage" },
    { to: "/sources", label: "Sources", icon: <Compass className="size-4" />, need: "library.manage" },
    { to: "/cleanup", label: "Cleanup", icon: <Eraser className="size-4" />, need: "admin" },
    {
      to: "/settings",
      label: "Settings",
      icon: <Settings className="size-4" />,
      need: "admin",
      children: [
        { to: "/settings/media", label: "Media management" },
        { to: "/settings/profiles", label: "Profiles" },
        { to: "/settings/sources", label: "Source modules" },
        { to: "/settings/search", label: "Search & throttling" },
        { to: "/settings/metadata", label: "Metadata" },
        { to: "/settings/library", label: "Library servers" },
        { to: "/settings/notifications", label: "Notifications" },
        { to: "/settings/upscalers", label: "Upscalers" },
        { to: "/settings/users", label: "Users & groups" },
        { to: "/settings/sso", label: "Single sign-on" },
        { to: "/settings/readers", label: "Readers" },
        { to: "/settings/reading", label: "Reading apps" },
        { to: "/settings/downloads", label: "Downloads" },
        { to: "/settings/schedule", label: "Schedule" },
        { to: "/settings/general", label: "General" },
      ],
    },
    {
      to: "/system",
      label: "System",
      icon: <Server className="size-4" />,
      need: "admin",
      children: [
        { to: "/system/status", label: "Status" },
        { to: "/system/tasks", label: "Tasks" },
        { to: "/system/backups", label: "Backups" },
        { to: "/system/database", label: "Database" },
        { to: "/system/logs", label: "Logs" },
      ],
    },
  ];

  const logout = async () => {
    await api.POST("/api/v1/auth/logout");
    qc.clear();
    window.location.reload();
  };

  const sidebar = (
    <nav className="flex h-full flex-col gap-0.5 p-3 text-sm">
      <div className="mb-4 flex items-center gap-2 px-2 pt-1">
        <img src="./favicon.svg" className="size-7" alt="" />
        <span className="text-lg font-semibold tracking-tight">mangarr</span>
      </div>
      {nav.filter((item) => !item.need || can(item.need)).map((item) => {
        const active = item.to === "/" ? loc.pathname === "/" || loc.pathname.startsWith("/series") : loc.pathname.startsWith(item.to);
        return (
          <div key={item.to}>
            <NavLink
              to={item.children ? item.children[0].to : item.to}
              onClick={() => setOpen(false)}
              className={clsx(
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 font-medium",
                active ? "bg-panel-2 text-fg" : "text-muted hover:bg-panel-2 hover:text-fg",
              )}
            >
              {item.icon}
              <span className="flex-1">{item.label}</span>
              {item.to === "/activity" && queued > 0 && (
                <span className={`rounded-full px-1.5 text-xs text-white ${queue?.state.paused ? "bg-warn" : "bg-accent"}`}>{queued}</span>
              )}
              {item.to === "/requests" && pendingRequests > 0 && <span className="rounded-full bg-accent px-1.5 text-xs text-white">{pendingRequests}</span>}
              {item.to === "/system" && issues > 0 && <span className="rounded-full bg-warn px-1.5 text-xs text-black">{issues}</span>}
            </NavLink>
            {item.children && active && (
              <div className="mb-1 ml-8 mt-0.5 flex flex-col border-l border-border">
                {item.children.map((c) => (
                  <NavLink
                    key={c.to}
                    to={c.to}
                    onClick={() => setOpen(false)}
                    className={({ isActive }) =>
                      clsx("-ml-px border-l px-3 py-1.5", isActive ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg")
                    }
                  >
                    {c.label}
                  </NavLink>
                ))}
              </div>
            )}
          </div>
        );
      })}
      <NavLink
        to="/account"
        onClick={() => setOpen(false)}
        className={({ isActive }) =>
          clsx("mt-auto flex items-center gap-2.5 rounded-md px-2.5 py-2 font-medium", isActive ? "bg-panel-2 text-fg" : "text-muted hover:bg-panel-2 hover:text-fg")
        }
      >
        <UserRound className="size-4" />
        <span className="flex-1 truncate">{name || "My account"}</span>
      </NavLink>
      <div className="flex items-center justify-between px-2 pt-2 text-xs text-muted">
        {can("library.manage") ? (
          <span className="flex items-center gap-1">
            <Activity className="size-3.5" /> {queued} in queue{queue?.state.paused ? " (paused)" : ""}
          </span>
        ) : (
          <span />
        )}
        <button className="flex items-center gap-1 hover:text-fg" onClick={logout}>
          <LogOut className="size-3.5" /> Log out
        </button>
      </div>
    </nav>
  );

  return (
    <div className="flex h-full">
      <aside className="hidden w-60 shrink-0 border-r border-border bg-panel md:block">{sidebar}</aside>
      {open && (
        <div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={() => setOpen(false)}>
          <aside className="h-full w-64 border-r border-border bg-panel" onClick={(e) => e.stopPropagation()}>
            {sidebar}
          </aside>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 items-center gap-3 border-b border-border px-4 md:hidden">
          <button onClick={() => setOpen(!open)} className="text-muted">
            {open ? <X className="size-5" /> : <Menu className="size-5" />}
          </button>
          <span className="font-semibold">mangarr</span>
        </header>
        <main className="min-w-0 flex-1 overflow-y-auto p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
