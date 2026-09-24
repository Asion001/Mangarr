import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createEventToastController } from "./eventToasts";
import { setLocale } from "./i18n/core";

function sink() {
  return {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  };
}

describe("server event toasts", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setLocale("en");
  });
  afterEach(() => vi.useRealTimers());

  it("groups chapter imports independently by title", () => {
    const toast = sink();
    const controller = createEventToastController(toast, { quietMs: 100, maxMs: 500 });
    controller.handle("chapter.imported", { seriesTitle: "One Piece", chapter: "1" });
    controller.handle("chapter.imported", { seriesTitle: "One Piece", chapter: "2" });
    controller.handle("chapter.imported", { seriesTitle: "Berserk", chapter: "1" });

    expect(toast.success).not.toHaveBeenCalled();
    vi.advanceTimersByTime(100);
    expect(toast.success).toHaveBeenCalledTimes(2);
    expect(toast.success).toHaveBeenCalledWith("One Piece: 2 chapters imported");
    expect(toast.success).toHaveBeenCalledWith("Berserk: 1 chapter imported");
  });

  it("flushes a continuous import batch at the maximum delay", () => {
    const toast = sink();
    const controller = createEventToastController(toast, { quietMs: 100, maxMs: 250 });
    controller.handle("chapter.imported", { seriesTitle: "Long batch" });
    vi.advanceTimersByTime(90);
    controller.handle("chapter.imported", { seriesTitle: "Long batch" });
    vi.advanceTimersByTime(90);
    controller.handle("chapter.imported", { seriesTitle: "Long batch" });
    vi.advanceTimersByTime(70);

    expect(toast.success).toHaveBeenCalledWith("Long batch: 3 chapters imported");
  });

  it("drops pending and immediate notices while suppressed", () => {
    const toast = sink();
    let reading = false;
    const controller = createEventToastController(toast, { quietMs: 100, suppressed: () => reading });
    controller.handle("chapter.imported", { seriesTitle: "Before reading" });
    reading = true;
    controller.handle("download.failed", { title: "Hidden failure" });
    vi.advanceTimersByTime(100);

    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("shows a new series and existing operational notices immediately", () => {
    const toast = sink();
    const controller = createEventToastController(toast);
    controller.handle("series.added", { title: "Series added", message: "Dorohedoro" });
    controller.handle("download.failed", { title: "Download failed", message: "Network" });
    controller.handle("health.issue", { title: "Health issue", message: "Disk" });
    controller.handle("cleanup.done", { title: "Cleanup done", message: "2 files" });

    expect(toast.success).toHaveBeenCalledWith("Series added", "Dorohedoro");
    expect(toast.error).toHaveBeenCalledWith("Download failed", "Network");
    expect(toast.warning).toHaveBeenCalledWith("Health issue", "Disk");
    expect(toast.info).toHaveBeenCalledWith("Cleanup done", "2 files");
  });

  it("uses the selected interface language for import digests", () => {
    setLocale("uk");
    const toast = sink();
    const controller = createEventToastController(toast, { quietMs: 100 });
    controller.handle("chapter.imported", { seriesTitle: "Берсерк" });
    controller.handle("chapter.imported", { seriesTitle: "Берсерк" });
    vi.advanceTimersByTime(100);

    expect(toast.success).toHaveBeenCalledWith("Берсерк: імпортовано розділів: 2");
  });
});
