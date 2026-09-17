import { t } from "../../lib/i18n/core";
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
        title={t("Downloads")}
        actions={
          <Button
            variant="primary"
            loading={dl.saving || rs.saving}
            onClick={async () => {
              await dl.save();
              if (rs.value) await rs.save();
            }}
          >{t("Save")}</Button>
        }
      />
      {dl.isLoading && <Loading />}
      {dl.error && <ErrorBox error={dl.error} />}
      {d && (
        <Card title={t("Queue")} className="mb-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Field env={dl.lock("maxConcurrent")} label={t("Parallel chapters")} help={t("Across all sources")}>
              <Input type="number" min={1} value={d.maxConcurrent} onChange={(e) => dl.patch({ maxConcurrent: Number(e.target.value) })} />
            </Field>
            <Field env={dl.lock("maxPerSource")} label={t("Parallel chapters per source")} help={t("Be gentle with sites")}>
              <Input type="number" min={1} value={d.maxPerSource} onChange={(e) => dl.patch({ maxPerSource: Number(e.target.value) })} />
            </Field>
            <Field env={dl.lock("pageConcurrency")} label={t("Parallel pages per chapter")}>
              <Input type="number" min={1} value={d.pageConcurrency} onChange={(e) => dl.patch({ pageConcurrency: Number(e.target.value) })} />
            </Field>
            <Field env={dl.lock("pageRetries")} label={t("Retries per page")}>
              <Input type="number" min={1} value={d.pageRetries} onChange={(e) => dl.patch({ pageRetries: Number(e.target.value) })} />
            </Field>
            <Field env={dl.lock("maxAttempts")} label={t("Attempts per release")} help={t("Then it is blocklisted and the next source is tried")}>
              <Input type="number" min={1} value={d.maxAttempts} onChange={(e) => dl.patch({ maxAttempts: Number(e.target.value) })} />
            </Field>
            <Field env={dl.lock("defaultCheckIntervalMinutes")} label={t("Check ongoing series every (min)")} help={t("Hiatus: daily, completed: weekly. Per-source overrides exist.")}>
              <Input type="number" min={15} value={d.defaultCheckIntervalMinutes} onChange={(e) => dl.patch({ defaultCheckIntervalMinutes: Number(e.target.value) })} />
            </Field>
          </div>
        </Card>
      )}
      {rs.value && (
        <Card title={t("Read progress")}>
          <Field env={rs.lock("intervalMinutes")} label={t("Sync reader progress every (min)")} help={t("From Komga/Kavita; used by read-based cleanup.")}>
            <Input type="number" min={5} className="max-w-40" value={rs.value.intervalMinutes} onChange={(e) => rs.patch({ intervalMinutes: Number(e.target.value) })} />
          </Field>
        </Card>
      )}
    </>
  );
}
