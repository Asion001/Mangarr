import { t as tr, t } from "../../lib/i18n/core";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { Copy, Download, FileText, LifeBuoy, RefreshCw } from "lucide-react";
import { api, apiUrl, unwrap } from "../../api/client";
import { Button, Loading, PageHeader, Select } from "../../components/ui";
import { bytes, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { useListParam } from "../../lib/urlState";

export function LogsPage() {
  const [levelParam, setLevel] = useListParam("level", "info");
  const level = levelParam as "debug" | "info" | "warn" | "error";
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["logs", level],
    queryFn: () => unwrap(api.GET("/api/v1/system/logs", { params: { query: { level, limit: 1000 } } })),
    refetchInterval: 10_000,
  });
  const files = useQuery({ queryKey: ["log-files"], queryFn: () => unwrap(api.GET("/api/v1/system/logs/files")), refetchInterval: 60_000 });
  const toast = useToast();
  const copy = async () => {
    const text = (data ?? [])
      .map((e) => `${e.time} ${e.level} ${e.message}${Object.entries(e.attrs ?? {}).map(([k, v]) => ` ${k}=${v}`).join("")}`)
      .join("\n");
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`Copied ${data?.length ?? 0} entries`);
    } catch (e) {
      toast.fromError(e, tr("Copy failed"));
    }
  };
  return (
    <>
      <PageHeader
        title={t("Logs")}
        actions={
          <>
            <Select className="w-32" value={level} onChange={(e) => setLevel(e.target.value)}>
              <option value="debug">{t("debug")}</option>
              <option value="info">{t("info")}</option>
              <option value="warn">{t("warn")}</option>
              <option value="error">{t("error")}</option>
            </Select>
            <Button icon={<RefreshCw className="size-4" />} loading={isFetching} onClick={() => refetch()}>{t("Refresh")}</Button>
            <Button icon={<Copy className="size-4" />} onClick={copy}>{t("Copy")}</Button>
            <a href={apiUrl("api/v1/system/diagnostics")} download title={t("Logs plus status, health, modules and settings, with secrets removed")}>
              <Button icon={<LifeBuoy className="size-4" />}>{t("Diagnostics")}</Button>
            </a>
            <a href={apiUrl("api/v1/system/logs/download")} download>
              <Button variant="primary" icon={<Download className="size-4" />}>{t("Download logs")}</Button>
            </a>
          </>
        }
      />
      <p className="mb-3 text-xs text-muted">{t("API keys, passwords and tokens are masked in this view, in copies and in downloads.")}{" "}
        {files.data && !files.data.enabled && tr("Log files are off (MANGARR_LOG_DIR=off): downloads contain the recent entries only.")}
      </p>
      {files.data?.enabled && files.data.files.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-2 text-xs">
          {files.data.files.map((f) => (
            <a
              key={f.name}
              href={apiUrl("api/v1/system/logs/download", { file: f.name })}
              download
              className="inline-flex items-center gap-1 rounded-md border border-border bg-panel px-2 py-1 hover:bg-panel-2"
              title={`updated ${relative(f.modified)}`}
            >
              <FileText className="size-3.5" /> {f.name} <span className="text-muted">{bytes(f.size)}</span>
            </a>
          ))}
        </div>
      )}
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
        {data?.length === 0 && <span className="text-muted">{t("No log entries at this level.")}</span>}
      </div>
    </>
  );
}
