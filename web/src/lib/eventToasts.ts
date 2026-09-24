import { plural, t } from "./i18n/core";

type ToastSink = {
  success: (title: string, message?: string) => void;
  error: (title: string, message?: string) => void;
  warning: (title: string, message?: string) => void;
  info: (title: string, message?: string) => void;
};

type Payload = {
  title?: string;
  message?: string;
  seriesTitle?: string;
};

type PendingImport = {
  title: string;
  count: number;
  firstAt: number;
  timer?: ReturnType<typeof setTimeout>;
};

type Options = {
  quietMs?: number;
  maxMs?: number;
  suppressed?: () => boolean;
};

export const chapterImportToastQuietMs = 2_000;
export const chapterImportToastMaxMs = 10_000;

/** Groups noisy chapter events while leaving the other server notices immediate. */
export function createEventToastController(toast: ToastSink, options: Options = {}) {
  const quietMs = options.quietMs ?? chapterImportToastQuietMs;
  const maxMs = options.maxMs ?? chapterImportToastMaxMs;
  const suppressed = options.suppressed ?? (() => false);
  const pending = new Map<string, PendingImport>();

  const discard = () => {
    for (const item of pending.values()) if (item.timer) clearTimeout(item.timer);
    pending.clear();
  };

  const flush = (key: string) => {
    const item = pending.get(key);
    if (!item) return;
    pending.delete(key);
    if (suppressed()) return;
    toast.success(plural(item.count, {
      one: t("{title}: {count} chapter imported", { title: item.title, count: item.count }),
      other: t("{title}: {count} chapters imported", { title: item.title, count: item.count }),
    }));
  };

  const chapterImported = (payload: Payload) => {
    const title = payload.seriesTitle?.trim() || t("Series");
    const key = title.toLocaleLowerCase();
    const item = pending.get(key) ?? { title, count: 0, firstAt: Date.now() };
    item.count++;
    if (item.timer) clearTimeout(item.timer);
    const remaining = Math.max(0, maxMs - (Date.now() - item.firstAt));
    item.timer = setTimeout(() => flush(key), Math.min(quietMs, remaining));
    pending.set(key, item);
  };

  const handle = (type: string, value: unknown) => {
    if (suppressed()) {
      discard();
      return;
    }
    const payload = (value ?? {}) as Payload;
    if (type === "chapter.imported") chapterImported(payload);
    if (type === "series.added") toast.success(payload.title ?? "Series added", payload.message);
    if (type === "download.failed") toast.error(payload.title ?? "Download failed", payload.message);
    if (type === "health.issue") toast.warning(payload.title ?? "Health issue", payload.message);
    if (type === "cleanup.done") toast.info(payload.title ?? "Cleanup done", payload.message);
  };

  return { handle, discard };
}
