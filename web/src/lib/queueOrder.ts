import type { Job } from "../api/client";

export const isPending = (job: Pick<Job, "status">) => job.status === "queued" || job.status === "paused";
export const isRunning = (job: Pick<Job, "status">) => ["downloading", "processing", "importing"].includes(job.status);

/** Neighbors follow the filtered queue order, including paused jobs. */
export function pendingNeighbor(items: Job[], id: number, direction: -1 | 1) {
  const index = items.findIndex((job) => job.id === id);
  if (index < 0 || !isPending(items[index])) return undefined;
  return items.slice(direction === -1 ? 0 : index + 1, direction === -1 ? index : undefined)
    .filter(isPending).at(direction === -1 ? -1 : 0);
}

/** Only join adjacent series so grouping never changes dispatch order. */
export function queueGroups(items: Job[]) {
  const groups: { title: string; seriesId: number; jobs: { job: Job; idx: number }[] }[] = [];
  items.forEach((job, idx) => {
    const last = groups.at(-1);
    if (last && last.seriesId === job.seriesId && isRunning(last.jobs[0].job) === isRunning(job)) {
      last.jobs.push({ job, idx });
    } else {
      groups.push({ title: job.seriesTitle, seriesId: job.seriesId, jobs: [{ job, idx }] });
    }
  });
  return groups;
}

/** The pending job just past a selection, skipping selected jobs: above the
 * first selected pending job (-1) or below the last one (1). */
export function selectionNeighbor(items: Job[], selected: Set<number>, direction: -1 | 1) {
  const pending = items.filter(isPending);
  const picked = pending.filter((job) => selected.has(job.id));
  if (!picked.length) return undefined;
  const edge = pending.indexOf(direction === -1 ? picked[0] : picked[picked.length - 1]);
  const rest = direction === -1 ? pending.slice(0, edge).reverse() : pending.slice(edge + 1);
  return rest.find((job) => !selected.has(job.id));
}

/** Queue position of each pending job (1 = next to run), counting jobs on
 * earlier pages; running and finished jobs have none. */
export function queuePositions(items: Job[], pageOffset: number, runningBefore: number) {
  const out = new Map<number, number>();
  items.forEach((job, i) => {
    if (isPending(job)) out.set(job.id, pageOffset + i + 1 - runningBefore);
  });
  return out;
}
