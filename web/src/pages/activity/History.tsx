import { useState } from "react";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { useSeriesList } from "../../api/queries";
import { Badge, Button, ErrorBox, Loading, PageHeader, Select, Table, Td, Th } from "../../components/ui";
import { dateTime } from "../../lib/format";

const tone: Record<string, "ok" | "warn" | "err" | "info" | "default" | "accent"> = {
  imported: "ok",
  upgraded: "info",
  grabbed: "default",
  failed: "err",
  blocklisted: "err",
  deleted: "warn",
  cleaned: "warn",
  restored: "info",
  upscaled: "accent",
  processed: "accent",
  moved: "info",
  progressRestored: "ok",
  readAhead: "info",
  unparsed: "warn",
};

export function HistoryPage() {
  const [page, setPage] = useState(1);
  const [eventType, setEventType] = useState("");
  const { data: series } = useSeriesList();
  const titles = new Map((series ?? []).map((s) => [s.id, s.title]));
  const { data, isLoading, error } = useQuery({
    queryKey: ["history", page, eventType],
    queryFn: () => unwrap(api.GET("/api/v1/history", { params: { query: { page, pageSize: 50, eventType: eventType || undefined } } })),
  });
  const pages = data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1;
  return (
    <>
      <PageHeader
        title="History"
        actions={
          <Select className="w-44" value={eventType} onChange={(e) => (setEventType(e.target.value), setPage(1))}>
            <option value="">All events</option>
            {Object.keys(tone).map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </Select>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && (
        <>
          <Table>
            <thead>
              <tr>
                <Th>Event</Th>
                <Th>Series</Th>
                <Th>Release</Th>
                <Th>Details</Th>
                <Th>Date</Th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((h) => (
                <tr key={h.id}>
                  <Td>
                    <Badge tone={tone[h.eventType] ?? "default"}>{h.eventType}</Badge>
                  </Td>
                  <Td>
                    <Link to={`/series/${h.seriesId}`} className="hover:text-accent-2">
                      {titles.get(h.seriesId) ?? `#${h.seriesId}`}
                    </Link>
                  </Td>
                  <Td className="max-w-xs truncate" >{h.sourceTitle}</Td>
                  <Td className="max-w-md text-xs text-muted">
                    {Object.entries(h.data ?? {})
                      .filter(([, v]) => v && v !== "false")
                      .map(([k, v]) => `${k}: ${v}`)
                      .join(" · ")}
                  </Td>
                  <Td className="whitespace-nowrap text-muted">{dateTime(h.createdAt)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
          <div className="mt-3 flex items-center justify-end gap-2 text-sm">
            <Button size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              Previous
            </Button>
            <span className="text-muted">
              {page} / {pages}
            </span>
            <Button size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>
              Next
            </Button>
          </div>
        </>
      )}
    </>
  );
}
