import { type S } from "../../api/client";
import { Button, Card, ErrorBox, Field, Input, Loading, PageHeader } from "../../components/ui";
import { useSettingsDoc } from "./useSettingsDoc";

type Downloads = S["Downloads"];
type ReadSync = S["ReadSync"];

export function DownloadsPage() {
  const dl = useSettingsDoc<Downloads>("downloads");
  const rs = useSettingsDoc<ReadSync>("readsync");
  const d = dl.value;
  return (
    <>
      <PageHeader
        title="Downloads"
        actions={
          <Button
            variant="primary"
            loading={dl.saving || rs.saving}
            onClick={async () => {
              await dl.save();
              if (rs.value) await rs.save();
            }}
          >
            Save
          </Button>
        }
      />
      {dl.isLoading && <Loading />}
      {dl.error && <ErrorBox error={dl.error} />}
      {d && (
        <Card title="Queue" className="mb-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Field label="Parallel chapters" help="Across all sources">
              <Input type="number" min={1} value={d.maxConcurrent} onChange={(e) => dl.patch({ maxConcurrent: Number(e.target.value) })} />
            </Field>
            <Field label="Parallel chapters per source" help="Be gentle with sites">
              <Input type="number" min={1} value={d.maxPerSource} onChange={(e) => dl.patch({ maxPerSource: Number(e.target.value) })} />
            </Field>
            <Field label="Parallel pages per chapter">
              <Input type="number" min={1} value={d.pageConcurrency} onChange={(e) => dl.patch({ pageConcurrency: Number(e.target.value) })} />
            </Field>
            <Field label="Retries per page">
              <Input type="number" min={1} value={d.pageRetries} onChange={(e) => dl.patch({ pageRetries: Number(e.target.value) })} />
            </Field>
            <Field label="Attempts per release" help="Then it is blocklisted and the next source is tried">
              <Input type="number" min={1} value={d.maxAttempts} onChange={(e) => dl.patch({ maxAttempts: Number(e.target.value) })} />
            </Field>
            <Field label="Check ongoing series every (min)" help="Hiatus: daily, completed: weekly. Per-source overrides exist.">
              <Input type="number" min={15} value={d.defaultCheckIntervalMinutes} onChange={(e) => dl.patch({ defaultCheckIntervalMinutes: Number(e.target.value) })} />
            </Field>
          </div>
        </Card>
      )}
      {rs.value && (
        <Card title="Read progress">
          <Field label="Sync reader progress every (min)" help="From Komga/Kavita; used by read-based cleanup.">
            <Input type="number" min={5} className="max-w-40" value={rs.value.intervalMinutes} onChange={(e) => rs.patch({ intervalMinutes: Number(e.target.value) })} />
          </Field>
        </Card>
      )}
    </>
  );
}
