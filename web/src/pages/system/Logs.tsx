import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { RefreshCw } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { Button, Loading, PageHeader, Select } from "../../components/ui";

export function LogsPage() {
  const [level, setLevel] = useState<"debug" | "info" | "warn" | "error">("info");
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["logs", level],
    queryFn: () => unwrap(api.GET("/api/v1/system/logs", { params: { query: { level, limit: 1000 } } })),
    refetchInterval: 10_000,
  });
  return (
    <>
      <PageHeader
        title="Logs"
        actions={
          <>
            <Select className="w-32" value={level} onChange={(e) => setLevel(e.target.value as typeof level)}>
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </Select>
            <Button icon={<RefreshCw className="size-4" />} loading={isFetching} onClick={() => refetch()}>
              Refresh
            </Button>
          </>
        }
      />
      {isLoading && <Loading />}
      <div className="overflow-x-auto rounded-lg border border-border bg-bg p-3 font-mono text-xs leading-relaxed">
        {data?.map((e, i) => (
          <div key={i} className="whitespace-pre-wrap break-all">
            <span className="text-muted">{new Date(e.time).toLocaleString()} </span>
            <span
              className={clsx(
                "font-semibold",
                e.level === "ERROR" && "text-err",
                e.level === "WARN" && "text-warn",
                e.level === "INFO" && "text-info",
                e.level === "DEBUG" && "text-muted",
              )}
            >
              {e.level.padEnd(5)}
            </span>{" "}
            {e.message}
            {e.attrs &&
              Object.entries(e.attrs).map(([k, v]) => (
                <span key={k} className="text-muted">
                  {" "}
                  {k}={v}
                </span>
              ))}
          </div>
        ))}
        {data?.length === 0 && <span className="text-muted">No log entries at this level.</span>}
      </div>
    </>
  );
}
