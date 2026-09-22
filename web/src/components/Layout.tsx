import { t, label } from "../lib/i18n/core";
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router";
import clsx from "clsx";
import { useQueryClient } from "@tanstack/react-query";
import {
  BookOpen,
  PanelLeftClose,
  PanelLeftOpen,
  Pencil,
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
  BellRing,
} from "lucide-react";
import { api } from "../api/client";
import { useHealth, usePendingRequests, useQueue } from "../api/queries";
import { useAccount, type Perm } from "../lib/account";

import { useUIMode } from "../lib/uiPreferences";
import { useToast } from "../lib/toast";

type NavItem = { to: string; label: string; icon: ReactNode; need?: Perm | Perm[]; children?: { to: string; label: string }[] };

export function Layout() {
  const [open, setOpen] = useState(false);
  const loc = useLocation();
  const navigate = useNavigate();
  const { editing, canEdit, save, saving } = useUIMode();
  const toast = useToast();
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem("mangarr:nav-collapsed") === "true");
  const drawer = useRef<HTMLElement>(null);
  const attemptedRoute = useRef("");
  const management = /^(\/add|\/activity|\/wanted|\/sources|\/settings|\/system|\/cleanup|\/import)(\/|$)/.test(loc.pathname);
  const routePermission: Perm[] = loc.pathname.startsWith("/add") ? ["library.manage", "requests.manage"] : /^(\/activity|\/wanted|\/sources)(\/|$)/.test(loc.pathname) ? ["library.manage"] : ["admin"];
  const routeKey = loc.pathname + loc.search;
  useEffect(() => {
    if (!management) { attemptedRoute.current = ""; return; }
    if (can(routePermission) && !editing && !saving && attemptedRoute.current !== routeKey) {
      attemptedRoute.current = routeKey;
      void save({mode:"editing"}).catch(e=>toast.fromError(e));
    }
  }, [routeKey, management, editing, saving, save]);
  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement as HTMLElement | null;
    const panel = drawer.current;
    const focusable = () => Array.from(panel?.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),select,input,[tabindex="0"]') ?? []).filter(el => el.getClientRects().length > 0);
    focusable()[0]?.focus();
    const keydown = (e:KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); setOpen(false); }
      if (e.key === "Tab") {
        const items = focusable(), first = items[0], last = items.at(-1);
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
      }
    };
    const desktop = window.matchMedia("(min-width: 768px)");
    const resized = () => { if (desktop.matches) setOpen(false); };
    document.addEventListener("keydown",keydown);
    desktop.addEventListener("change",resized);
    return () => { document.removeEventListener("keydown",keydown); desktop.removeEventListener("change",resized); previous?.focus(); };
  }, [open]);
  const switchMode = async () => {
    try {
      if (editing && management) navigate("/");
      await save({mode:editing?"reading":"editing"});
    } catch(e) { toast.fromError(e); }
  };
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
    { to: "/discover", label: "Discover", icon: <Compass className="size-4" /> },
    { to: "/updates", label: "Updates", icon: <BellRing className="size-4" /> },
    { to: "/add", label: "Add series", icon: <PlusCircle className="size-4" />, need: "library.manage" },
    { to: "/requests", label: "Requests", icon: <Inbox className="size-4" />, need: ["requests.create", "requests.manage", "library.manage"] },
    { to: "/import", label: "Import library", icon: <FileUp className="size-4" />, need: "admin" },
    {
      to: "/activity",
      label: "Activity",
      icon: <Download className="size-4" />,
      need: "library.manage",
      children: [
        { to: "/activity/downloads", label: "Downloads" },
        { to: "/activity/processing", label: "Processing" },
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
        { to: "/system/workers", label: "Workers" },
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

  const sidebar = (mobile = false) => {
    const compact = collapsed && !mobile;
    return <div className="flex h-full min-h-0 flex-col p-3 text-sm" style={mobile ? {paddingTop:"max(0.75rem, env(safe-area-inset-top))",paddingBottom:"max(0.75rem, env(safe-area-inset-bottom))"} : undefined}>
      <div className="mb-3 flex shrink-0 items-center gap-2 px-1">
        {!compact && <><img src="./favicon.svg" className="size-7" alt="" /><span className="flex-1 text-lg font-semibold">mangarr</span></>}
        <button className="rounded p-2 text-muted hover:bg-panel-2 hover:text-fg" aria-label={mobile?t("Close navigation"):compact?t("Expand navigation"):t("Collapse navigation")} title={mobile?t("Close navigation"):compact?t("Expand navigation"):t("Collapse navigation")} onClick={()=>{
          if(mobile)setOpen(false);
          else {setCollapsed(!collapsed);localStorage.setItem("mangarr:nav-collapsed",String(!collapsed));}
        }}>{mobile?<X className="size-4"/>:compact?<PanelLeftOpen className="size-4"/>:<PanelLeftClose className="size-4"/>}</button>
      </div>
      <nav aria-label={t("Navigation")} className="min-h-0 flex-1 space-y-0.5 overflow-y-auto overscroll-contain [&_a:focus-visible]:-outline-offset-2">
        {nav.filter(item => (!item.need || can(item.need)) && (editing || item.to === "/" || item.to === "/discover" || item.to === "/updates" || (item.to === "/requests" && can("requests.create")))).map(item=>{
          const active = item.to === "/" ? loc.pathname === "/" || loc.pathname.startsWith("/series") : loc.pathname.startsWith(item.to);
          const count = item.to === "/activity" ? queued : item.to === "/requests" && editing ? pendingRequests : item.to === "/system" ? issues : 0;
          return <div key={item.to}>
            <NavLink to={item.children?item.children[0].to:item.to} title={label(item.label)} aria-label={label(item.label)} onClick={()=>setOpen(false)} className={clsx("flex min-h-10 items-center gap-2.5 rounded-md px-2.5 py-2 font-medium",compact&&"justify-center",active?"bg-panel-2 text-fg":"text-muted hover:bg-panel-2 hover:text-fg")}>
              <span className="shrink-0">{item.icon}</span>
              {!compact&&<><span className="flex-1">{label(item.label)}</span>{count>0&&<span className="rounded-full bg-primary px-1.5 text-xs font-medium text-white">{count}</span>}</>}
            </NavLink>
            {!compact&&item.children&&active&&<div className="mb-1 ml-8 mt-0.5 flex flex-col border-l border-border">
              {item.children.map(c=><NavLink key={c.to} to={c.to} onClick={()=>setOpen(false)} className={({isActive})=>clsx("-ml-px border-l px-3 py-1.5",isActive?"border-accent text-fg":"border-transparent text-muted hover:text-fg")}>{label(c.label)}</NavLink>)}
            </div>}
          </div>;
        })}
      </nav>
      <footer className="mt-2 shrink-0 space-y-1 border-t border-border pt-2">
        {canEdit&&<button disabled={saving} title={editing?t("Switch to reading mode"):t("Switch to editing mode")} aria-label={editing?t("Switch to reading mode"):t("Switch to editing mode")} onClick={()=>void switchMode()} className={clsx("flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-muted hover:bg-panel-2 disabled:opacity-50",compact&&"justify-center")}>
          {editing?<Pencil className="size-4 shrink-0"/>:<BookOpen className="size-4 shrink-0"/>}{!compact&&<span>{editing?t("Editing mode"):t("Reading mode")}</span>}
        </button>}
        <NavLink to="/account" title={name||t("My account")} aria-label={t("My account")} onClick={()=>setOpen(false)} className={({isActive})=>clsx("flex items-center gap-2.5 rounded-md px-2.5 py-2 font-medium",compact&&"justify-center",isActive?"bg-panel-2 text-fg":"text-muted hover:bg-panel-2 hover:text-fg")}>
          <UserRound className="size-4 shrink-0"/>{!compact&&<span className="min-w-0 flex-1 truncate">{name||t("My account")}</span>}
        </NavLink>
        <div className={clsx("flex items-center px-2 text-xs text-muted",compact?"justify-center":"justify-between")}>
          {!compact&&<span>{editing&&can("library.manage")&&<span className="flex items-center gap-1"><Activity className="size-3.5"/>{queued} {t("in queue")}</span>}</span>}
          <button title={t("Log out")} aria-label={t("Log out")} className="flex items-center gap-1 py-2 hover:text-fg" onClick={logout}><LogOut className="size-3.5"/>{!compact&&t("Log out")}</button>
        </div>
      </footer>
    </div>;
  };

  // --nav-width lets fixed bars in pages line up with the content column
  return <div className="flex h-dvh overflow-hidden" style={{"--nav-width":collapsed?"4rem":"15rem"} as CSSProperties}>
    <aside data-testid="desktop-navigation" className={clsx("hidden shrink-0 border-r border-border bg-panel md:block",collapsed?"w-16":"w-60")}>{sidebar()}</aside>
    {open&&<div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={()=>setOpen(false)}>
      <aside ref={drawer} role="dialog" aria-modal="true" aria-label={t("Navigation")} className="h-dvh w-72 max-w-[85vw] border-r border-border bg-panel" onClick={e=>e.stopPropagation()}>{sidebar(true)}</aside>
    </div>}
    <div inert={open} className="flex min-w-0 flex-1 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4 md:hidden">
        <button onClick={()=>setOpen(true)} aria-label={t("Open navigation")} aria-expanded={open} className="p-2 text-muted"><Menu className="size-5"/></button>
        <span className="font-semibold">mangarr</span>
      </header>
      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4 md:p-6"><Outlet/></main>
    </div>
  </div>;
}
