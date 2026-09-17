import { t } from "../../lib/i18n/core";
import { Link } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { EmptyState, ErrorBox, IconButton, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { dateTime } from "../../lib/format";

export function BlocklistPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({ queryKey: ["blocklist"], queryFn: () => unwrap(api.GET("/api/v1/blocklist")) });
  const remove = async (id: number) => {
    await unwrap(api.DELETE("/api/v1/blocklist/{id}", { params: { path: { id } } }));
    qc.invalidateQueries({ queryKey: ["blocklist"] });
  };
  return (
    <>
      <PageHeader title={t("Blocklist")} subtitle={t("Releases that failed or were rejected; they are never downloaded again.")} />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <EmptyState title={t("Blocklist is empty")} />}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t("Series")}</Th>
              <Th>{t("Source")}</Th>
              <Th>{t("Scanlator")}</Th>
              <Th>{t("Reason")}</Th>
              <Th>{t("Date")}</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((b) => (
              <tr key={b.id}>
                <Td>
                  <Link to={`/series/${b.seriesId}`} className="hover:text-accent-2">
                    {b.seriesTitle}
                  </Link>
                </Td>
                <Td>{b.sourceName}</Td>
                <Td className="text-muted">{b.scanlator || "—"}</Td>
                <Td className="max-w-md text-xs text-err">{b.reason}</Td>
                <Td className="whitespace-nowrap text-muted">{dateTime(b.createdAt)}</Td>
                <Td className="text-right">
                  <IconButton title={t("Remove from blocklist")} onClick={() => remove(b.id)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
