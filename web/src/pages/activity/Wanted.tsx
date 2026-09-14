import { useState } from "react";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { usePushCommand } from "../../api/queries";
import { Badge, Button, EmptyState, ErrorBox, IconButton, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { date } from "../../lib/format";

export function WantedPage() {
  const [page, setPage] = useState(1);
  const push = usePushCommand();
  const { data, isLoading, error } = useQuery({
    queryKey: ["wanted", page],
    queryFn: () => unwrap(api.GET("/api/v1/wanted/missing", { params: { query: { page, pageSize: 50 } } })),
  });
  const pages = data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1;
  return (
    <>
      <PageHeader
        title="Wanted"
        subtitle={data ? `${data.total} monitored chapters are missing` : undefined}
        actions={
          <Button icon={<Search className="size-4" />} onClick={() => push.mutate({ name: "SearchMissing", label: "Searching all missing chapters" })}>
            Search all
          </Button>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.items.length === 0 && <EmptyState title="Nothing is missing">Every monitored chapter is downloaded.</EmptyState>}
      {data && data.items.length > 0 && (
        <>
          <Table>
            <thead>
              <tr>
                <Th>Series</Th>
                <Th>Chapter</Th>
                <Th>Title</Th>
                <Th>Released</Th>
                <Th>Releases</Th>
                <Th>State</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {data.items.map((c) => (
                <tr key={c.id}>
                  <Td>
                    <Link to={`/series/${c.seriesId}`} className="hover:text-accent-2">
                      {c.seriesTitle}
                    </Link>
                  </Td>
                  <Td className="font-mono text-xs">{c.number}</Td>
                  <Td className="max-w-xs truncate">{c.title}</Td>
                  <Td className="text-muted">{date(c.releaseDate)}</Td>
                  <Td>{c.releases}</Td>
                  <Td>
                    <Badge tone={c.state === "failed" ? "err" : c.state === "missing" ? "warn" : "info"}>{c.state}</Badge>
                  </Td>
                  <Td className="text-right">
                    <IconButton
                      title="Search"
                      onClick={() => push.mutate({ name: "SearchMissing", body: { seriesId: c.seriesId, chapterIds: [c.id], explicit: true }, label: `Searching ch. ${c.number}` })}
                    >
                      <Search className="size-4" />
                    </IconButton>
                  </Td>
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
