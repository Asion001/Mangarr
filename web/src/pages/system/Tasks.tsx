import { t as tr, t as translateUI } from "../../lib/i18n/core";
import { Play } from "lucide-react";
import { usePushCommand, useTasks } from "../../api/queries";
import { Button, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";

export function TasksPage() {
  const { data, isLoading } = useTasks();
  const push = usePushCommand();
  const needsBody = new Set(["RefreshSeries", "UpscaleExisting"]);
  return (
    <>
      <PageHeader title={translateUI("Tasks")} subtitle={translateUI("Scheduled and on-demand commands")} />
      {isLoading && <Loading />}
      {data && (
        <Table>
          <thead>
            <tr>
              <Th>{translateUI("Task")}</Th>
              <Th>{translateUI("Interval")}</Th>
              <Th>{translateUI("Last run")}</Th>
              <Th>{translateUI("Next run")}</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((t) => (
              <tr key={t.name}>
                <Td>
                  <div className="font-medium">{t.name}</div>
                  <div className="text-xs text-muted">{t.description}</div>
                </Td>
                <Td className="text-muted">{t.scheduled ? (t.intervalMinutes >= 60 ? `${t.intervalMinutes / 60} h` : `${t.intervalMinutes} min`) : tr("manual")}</Td>
                <Td className="text-muted">{t.scheduled ? relative(t.lastExecution) : "—"}</Td>
                <Td className="text-muted">{t.scheduled ? relative(t.nextExecution) : "—"}</Td>
                <Td className="text-right">
                  {!needsBody.has(t.name) && (
                    <Button size="sm" icon={<Play className="size-3.5" />} onClick={() => push.mutate({ name: t.name, label: `${t.name} queued` })}>{translateUI("Run")}</Button>
                  )}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
