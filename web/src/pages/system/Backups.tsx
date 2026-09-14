import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Plus, Trash2 } from "lucide-react";
import { api, apiUrl, unwrap } from "../../api/client";
import { Badge, Button, EmptyState, IconButton, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { bytes, dateTime } from "../../lib/format";
import { useToast } from "../../lib/toast";

export function BackupsPage() {
  const qc = useQueryClient();
  const toast = useToast();
  const [creating, setCreating] = useState(false);
  const { data, isLoading } = useQuery({ queryKey: ["backups"], queryFn: () => unwrap(api.GET("/api/v1/system/backups")) });
  const create = async () => {
    setCreating(true);
    try {
      await unwrap(api.POST("/api/v1/system/backups"));
      qc.invalidateQueries({ queryKey: ["backups"] });
      toast.success("Backup created");
    } catch (e) {
      toast.fromError(e);
    } finally {
      setCreating(false);
    }
  };
  const remove = async (name: string) => {
    await unwrap(api.DELETE("/api/v1/system/backups/{name}", { params: { path: { name } } }));
    qc.invalidateQueries({ queryKey: ["backups"] });
  };
  return (
    <>
      <PageHeader
        title="Backups"
        subtitle="Database and settings. With PostgreSQL, back up the database with pg_dump."
        actions={
          <Button variant="primary" loading={creating} icon={<Plus className="size-4" />} onClick={create}>
            Back up now
          </Button>
        }
      />
      {isLoading && <Loading />}
      {data?.length === 0 && <EmptyState title="No backups yet" />}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Type</Th>
              <Th>Size</Th>
              <Th>Created</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((b) => (
              <tr key={b.name}>
                <Td className="font-mono text-xs">{b.name}</Td>
                <Td>
                  <Badge>{b.type}</Badge>
                </Td>
                <Td>{bytes(b.size)}</Td>
                <Td className="text-muted">{dateTime(b.created)}</Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    <a href={apiUrl(`api/v1/system/backups/${encodeURIComponent(b.name)}`)} download>
                      <IconButton title="Download">
                        <Download className="size-4" />
                      </IconButton>
                    </a>
                    <IconButton title="Delete" onClick={() => remove(b.name)}>
                      <Trash2 className="size-4" />
                    </IconButton>
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
