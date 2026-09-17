import { throughput } from "./processingMetrics";
import { useEffect, useSyncExternalStore } from "react";
import type { S } from "../api/client";
import { bytes } from "./format";
import { onServerEvent } from "./events";

export type LiveProgress = S["LiveProgress"];

// job id -> progress, fed by "processing.progress" server events
let jobs = new Map<number, LiveProgress>();
const subs = new Set<() => void>();
let started = false;

function start() {
  if (started) return;
  started = true;
  onServerEvent((type, payload) => {
    if (type !== "processing.progress") return;
    const p = payload as LiveProgress;
    const next = new Map(jobs);
    if (p.stage === "done") next.delete(p.jobId);
    else next.set(p.jobId, p);
    jobs = next;
    subs.forEach((f) => f());
  });
}

/** useLiveProgress returns the live progress of running jobs by job id. */
export function useLiveProgress(): Map<number, LiveProgress> {
  useEffect(start, []);
  return useSyncExternalStore(
    (f) => {
      subs.add(f);
      return () => subs.delete(f);
    },
    () => jobs,
  );
}

const stageLabel: Record<string, string> = { download: "downloading", upscale: "upscaling", encode: "encoding", write: "writing" };

/** describe summarizes live progress: "encoding 34/60 · 3.1 p/s · −42% · 12s left". */
export function describe(p: LiveProgress): string {
  const parts = [`${stageLabel[p.stage] ?? p.stage} ${p.done}/${p.total}`];
  if (p.rate > 0) parts.push(throughput(p.rate,1));
  if (p.bytesIn > 0 && p.bytesOut > 0 && p.stage === "encode") parts.push(`${Math.round((p.bytesOut / p.bytesIn - 1) * 100)}%`);
  if (p.bytesIn > 0 && p.stage === "download") parts.push(bytes(p.bytesIn));
  if (p.eta > 0) parts.push(`${eta(p.eta)} left`);
  return parts.join(" · ");
}

export function eta(seconds: number): string {
  const s = Math.round(seconds);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  const h = Math.floor(s / 3600);
  if (h < 48) return `${h}h ${Math.round((s % 3600) / 60)}m`;
  return `${Math.round(h / 24)} days`;
}
