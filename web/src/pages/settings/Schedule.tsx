import { Plus, Trash2 } from "lucide-react";
import { type S } from "../../api/client";
import { Button, Card, EmptyState, EnvLock, ErrorBox, Field, IconButton, Input, Loading, Locked, PageHeader, Select, Switch } from "../../components/ui";
import { useSettingsDoc } from "./useSettingsDoc";

type Schedule = S["Schedule"];
type Window = S["ScheduleWindow"];
const days = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

export function SchedulePage() {
  const doc = useSettingsDoc<Schedule>("schedule");
  const v = doc.value;
  const windows = v?.windows ?? [];
  const setWindow = (i: number, p: Partial<Window>) => doc.patch({ windows: windows.map((w, j) => (j === i ? { ...w, ...p } : w)) });
  const env = doc.lock("windows");
  return (
    <>
      <PageHeader
        title="Schedule"
        subtitle="Quiet hours: pause downloads or processing, or throttle more gently, at certain times."
        actions={
          <Button variant="primary" loading={doc.saving} onClick={() => doc.save()}>
            Save
          </Button>
        }
      />
      {doc.isLoading && <Loading />}
      {doc.error && <ErrorBox error={doc.error} />}
      {v && (
        <>
          <Card className="mb-4">
            <Field label="Time zone" help="IANA name such as Europe/Madrid. Empty = the server's time zone (TZ)." env={doc.lock("timezone")}>
              <Input className="max-w-xs" value={v.timezone} placeholder={Intl.DateTimeFormat().resolvedOptions().timeZone} onChange={(e) => doc.patch({ timezone: e.target.value })} />
            </Field>
          </Card>
          <Card
            title={
              <span className="inline-flex items-center gap-2">
                Windows <EnvLock env={env} />
              </span>
            }
            actions={
              <Button
                size="sm"
                disabled={!!env}
                icon={<Plus className="size-3.5" />}
                onClick={() =>
                  doc.patch({ windows: [...windows, { name: "Night", days: [], start: "01:00", end: "07:00", pauseDownloads: false, pauseProcessing: false, throttle: "" }] })
                }
              >
                Add window
              </Button>
            }
          >
            <Locked env={env}>
              {windows.length === 0 && <EmptyState title="No quiet hours">Everything runs around the clock.</EmptyState>}
              <div className="flex flex-col gap-4">
                {windows.map((w, i) => (
                  <div key={i} className="rounded-lg border border-border p-3">
                    <div className="mb-3 flex flex-wrap items-end gap-3">
                      <Field label="Name" className="w-40">
                        <Input value={w.name} onChange={(e) => setWindow(i, { name: e.target.value })} />
                      </Field>
                      <Field label="From">
                        <Input type="time" value={w.start} onChange={(e) => setWindow(i, { start: e.target.value })} />
                      </Field>
                      <Field label="To" help={w.end <= w.start ? "next day" : undefined}>
                        <Input type="time" value={w.end} onChange={(e) => setWindow(i, { end: e.target.value })} />
                      </Field>
                      <IconButton title="Remove window" className="ml-auto" onClick={() => doc.patch({ windows: windows.filter((_, j) => j !== i) })}>
                        <Trash2 className="size-4" />
                      </IconButton>
                    </div>
                    <div className="mb-3 flex flex-wrap gap-1">
                      {days.map((d) => {
                        const on = (w.days ?? []).includes(d);
                        return (
                          <button
                            key={d}
                            type="button"
                            onClick={() => setWindow(i, { days: on ? w.days.filter((x) => x !== d) : [...(w.days ?? []), d] })}
                            className={`rounded border px-2 py-0.5 text-xs ${on ? "border-accent bg-accent/15 text-fg" : "border-border text-muted"}`}
                          >
                            {d}
                          </button>
                        );
                      })}
                      <span className="self-center text-xs text-muted">{(w.days ?? []).length === 0 && "every day"}</span>
                    </div>
                    <div className="flex flex-wrap items-center gap-4">
                      <Switch checked={w.pauseDownloads} onChange={(x) => setWindow(i, { pauseDownloads: x })} label="Pause downloads" />
                      <Switch checked={w.pauseProcessing} onChange={(x) => setWindow(i, { pauseProcessing: x })} label="Pause upscaling / re-encoding" />
                      <label className="flex items-center gap-2 text-sm">
                        Throttle
                        <Select className="w-36" value={w.throttle ?? ""} onChange={(e) => setWindow(i, { throttle: e.target.value as Window["throttle"] })}>
                          <option value="">unchanged</option>
                          <option value="gentle">gentle</option>
                          <option value="normal">normal</option>
                          <option value="fast">fast</option>
                        </Select>
                      </label>
                    </div>
                  </div>
                ))}
              </div>
              <p className="mt-3 text-xs text-muted">
                Example: to upscale only at night, add a window 07:00–01:00 with “Pause upscaling”. A window that ends before it starts continues into the next day.
              </p>
            </Locked>
          </Card>
        </>
      )}
    </>
  );
}
