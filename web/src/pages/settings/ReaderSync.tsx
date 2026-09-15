import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, KeyRound, Server, Smartphone } from "lucide-react";
import { Link } from "react-router";
import { api, unwrap, type S } from "../../api/client";
import { Badge, ErrorBox, Spinner } from "../../components/ui";
import { relative } from "../../lib/format";

type Event = S["EventView"];
type Device = S["DeviceSync"];

const originLabel: Record<string, string> = { app: "Reading app", server: "Library server", backup: "Backup import" };

/** What an event says happened. */
function describe(e: Event): string {
  const ch = e.chapter ? (e.chapters > 1 ? `up to ch. ${e.chapter}` : `ch. ${e.chapter}`) : "a chapter";
  switch (e.outcome) {
    case "unread":
      return e.chapters > 1 ? `${e.chapters} chapters marked unread` : `${ch} marked unread`;
    case "kept":
      return `reported ${ch} ${e.completed ? "read" : `at page ${e.page}`}, lower than mangarr: kept mangarr's`;
    default:
      if (e.completed) return e.chapters > 1 ? `${e.chapters} chapters read, ${ch}` : `read ${ch}`;
      return `${ch}, page ${e.page}`;
  }
}

function who(d: { client: string; device: string; origin: string }) {
  const name = d.client || originLabel[d.origin] || d.origin;
  if (!d.device || d.device === name) return name;
  // a device named after its app ("KMReader iPad") says it all
  const app = name.split(" ")[0].toLowerCase();
  return d.device.toLowerCase().includes(app) ? d.device : `${name} · ${d.device}`;
}

function OriginIcon({ origin }: { origin: string }) {
  const cls = "size-4 shrink-0 text-muted";
  return origin === "server" ? <Server className={cls} /> : origin === "app" ? <Smartphone className={cls} /> : <KeyRound className={cls} />;
}

function EventLine({ e }: { e: Event }) {
  return (
    <span className="min-w-0">
      {e.seriesTitle ? (
        <Link to={`/series/${e.seriesId}`} className="hover:underline">
          {e.seriesTitle}
        </Link>
      ) : (
        <span className="text-muted">deleted series</span>
      )}
      <span className="text-muted">: {describe(e)}</span>
    </span>
  );
}

function DeviceRow({ d }: { d: Device }) {
  return (
    <div className="flex items-start gap-2 rounded bg-panel-2 px-3 py-2 text-sm">
      <OriginIcon origin={d.origin} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{who(d)}</span>
          <span className="text-xs text-muted">last report {relative(d.lastSeen)}</span>
          {d.kept > 0 && (
            <Badge tone="warn" title="Reports lower than mangarr's progress, for example a server that doesn't know about chapters read in an app yet. mangarr kept its own progress.">
              {d.kept} kept
            </Badge>
          )}
        </div>
        {d.last && (
          <div className="truncate text-xs">
            <EventLine e={d.last} />
          </div>
        )}
      </div>
    </div>
  );
}

/** ReaderSyncPanel shows who reports a reader's progress and what they reported. */
export function ReaderSyncPanel({ readerId }: { readerId: number }) {
  const [open, setOpen] = useState(false);
  const [showEvents, setShowEvents] = useState(false);
  const { data, isLoading, error } = useQuery({
    queryKey: ["readers", readerId, "sync"],
    queryFn: () => unwrap(api.GET("/api/v1/readers/{id}/sync", { params: { path: { id: readerId }, query: { limit: 30 } } })),
    enabled: open,
  });
  const unusedKeys = (data?.keys ?? []).filter((k) => !data?.devices.some((d) => d.origin === "app" && d.device === k.comment));
  return (
    <div className="border-t border-border pt-3">
      <button type="button" className="flex items-center gap-1 text-sm font-medium text-muted hover:text-fg" onClick={() => setOpen(!open)}>
        {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
        Devices & sync
      </button>
      {open && (
        <div className="mt-2 flex flex-col gap-2">
          {isLoading && <Spinner />}
          {error && <ErrorBox error={error} />}
          {data?.readingApps && (
            <p className="text-xs text-muted">
              Reading apps (Mihon, KMReader, Paperback) act as this reader. <Link to="/settings/reading" className="text-accent-2 hover:underline">Reading apps settings</Link>
            </p>
          )}
          {data && data.devices.length === 0 && unusedKeys.length === 0 && <p className="text-sm text-muted">No progress reports in the last 30 days.</p>}
          {data?.devices.map((d) => <DeviceRow key={`${d.origin}/${d.client}/${d.device}`} d={d} />)}
          {unusedKeys.map((k) => (
            <div key={k.id} className="flex items-center gap-2 rounded bg-panel-2 px-3 py-2 text-sm">
              <Smartphone className="size-4 shrink-0 text-muted" />
              <span className="font-medium">{k.comment || k.prefix}</span>
              <span className="text-xs text-muted">
                {k.lastUsedAt ? `connected ${relative(k.lastUsedAt)}${k.lastClient ? ` (${k.lastClient})` : ""}, no progress yet` : "key not used yet"}
              </span>
            </div>
          ))}
          {data && data.events.length > 0 && (
            <div>
              <button type="button" className="flex items-center gap-1 text-xs text-muted hover:text-fg" onClick={() => setShowEvents(!showEvents)}>
                {showEvents ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                Recent reports
              </button>
              {showEvents && (
                <ul className="mt-1 flex flex-col gap-2 text-xs">
                  {data.events.map((e) => (
                    <li key={e.id} className="flex flex-col">
                      <span className="flex items-center gap-2 text-muted">
                        <span title={e.at}>{relative(e.at)}</span>· <span className="truncate">{who(e)}</span>
                        {e.outcome === "kept" && <Badge tone="warn">kept</Badge>}
                      </span>
                      <EventLine e={e} />
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
