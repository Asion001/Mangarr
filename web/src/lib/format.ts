export function bytes(n?: number | null): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function date(s?: string | null): string {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d.getTime()) || d.getFullYear() < 1971) return "—";
  return d.toLocaleDateString();
}

export function dateTime(s?: string | null): string {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d.getTime()) || d.getFullYear() < 1971) return "—";
  return d.toLocaleString();
}

export function relative(s?: string | null): string {
  if (!s) return "never";
  const d = new Date(s).getTime();
  if (isNaN(d)) return "never";
  const diff = (Date.now() - d) / 1000;
  const abs = Math.abs(diff);
  const fmt = (v: number, u: string) => `${Math.round(v)} ${u}${Math.round(v) === 1 ? "" : "s"}`;
  let out: string;
  if (abs < 60) out = "just now";
  else if (abs < 3600) out = fmt(abs / 60, "minute");
  else if (abs < 86400) out = fmt(abs / 3600, "hour");
  else out = fmt(abs / 86400, "day");
  if (out === "just now") return out;
  return diff >= 0 ? `${out} ago` : `in ${out}`;
}

export function duration(ms?: number | null): string {
  if (!ms) return "0s";
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

export function titleCase(s: string): string {
  return s.replace(/[-_.]/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}
