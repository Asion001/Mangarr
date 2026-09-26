import { describe, expect, it } from "vitest";
import type { Job } from "../api/client";
import { isPending, isRunning, pendingNeighbor, queueGroups } from "./queueOrder";

const job = (id: number, status = "queued", seriesId = 1, kind = "download") => ({ id, status, seriesId, seriesTitle: `Series ${seriesId}`, rank: id * 1024, kind }) as Job;

describe("queue order", () => {
  it.each(["download", "reprocess"])("finds pending neighbors for %s without moving active or finished jobs", (kind) => {
    const items = [job(4, "downloading", 1, kind), job(1, "queued", 1, kind), job(2, "paused", 2, kind), job(3, "queued", 1, kind), job(5, "failed", 1, kind)];
    expect(pendingNeighbor(items, 2, -1)?.id).toBe(1);
    expect(pendingNeighbor(items, 2, 1)?.id).toBe(3);
    expect(pendingNeighbor(items, 1, -1)).toBeUndefined();
    expect(pendingNeighbor(items, 3, 1)).toBeUndefined();
    expect(pendingNeighbor(items, 4, 1)).toBeUndefined();
    expect(pendingNeighbor(items, 5, -1)).toBeUndefined();
    expect(pendingNeighbor(items, 999, 1)).toBeUndefined();
  });

  it("preserves rank order and pinned work when series are interleaved", () => {
    const items = [job(5, "processing"), job(6, "downloading", 2), job(1), job(2, "queued", 2), job(3), job(4)];
    const groups = queueGroups(items);
    expect(groups.flatMap((group) => group.jobs.map(({ job }) => job.id))).toEqual([5, 6, 1, 2, 3, 4]);
    expect(groups.map((group) => group.jobs.length)).toEqual([1, 1, 1, 1, 2]);
    expect(groups.flatMap((group) => group.jobs.map(({ idx }) => idx))).toEqual([0, 1, 2, 3, 4, 5]);
  });

  it("separates running and pending jobs even within the same series", () => {
    expect(queueGroups([job(2, "importing"), job(1)])).toHaveLength(2);
    for (const status of ["downloading", "processing", "importing"]) {
      expect(isRunning(job(1, status))).toBe(true);
      expect(isPending(job(1, status))).toBe(false);
    }
    expect(queueGroups([])).toEqual([]);
  });
});
