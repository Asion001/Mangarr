import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { BookOpen, BookPlus, Sparkles } from "lucide-react";
import { api, apiUrl, unwrap, type S } from "../../api/client";
import { Cover } from "../../components/Cover";
import { Badge, Button, Card, ErrorBox, Loading, PageHeader, Select } from "../../components/ui";
import { relative } from "../../lib/format";
import { t } from "../../lib/i18n/core";

type Update = S["UpdateItem"];

export function UpdatesPage() {
  const [days, setDays] = useState(30);
  const { data, isLoading, error } = useQuery({
    queryKey: ["updates", days],
    queryFn: () => unwrap(api.GET("/api/v1/updates", { params: { query: { days, limit: 200 } } })),
    staleTime: 60_000,
  });
  const groups = groupByDay(data ?? []);
  return (
    <>
      <PageHeader
        title={t("Updates")}
        subtitle={t("New chapters discovered and new titles added to your library.")}
        actions={
          <Select className="w-40" value={days} onChange={(event) => setDays(Number(event.target.value))}>
            <option value={7}>{t("Last 7 days")}</option>
            <option value={30}>{t("Last 30 days")}</option>
            <option value={90}>{t("Last 90 days")}</option>
          </Select>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {!isLoading && !error && groups.length === 0 && (
        <Card><p className="text-sm text-muted">{t("No updates in this period.")}</p></Card>
      )}
      <div className="flex flex-col gap-6">
        {groups.map(([day, items]) => (
          <section key={day}>
            <h2 className="mb-2 text-sm font-semibold text-muted">{day}</h2>
            <Card className="divide-y divide-border !p-0">
              {items.map((item, index) => <UpdateRow key={`${item.kind}-${item.chapterId || item.seriesId}-${index}`} item={item} />)}
            </Card>
          </section>
        ))}
      </div>
    </>
  );
}

function UpdateRow({ item }: { item: Update }) {
  const chapter = item.kind === "chapter";
  return (
    <div className="flex items-center gap-3 p-3 sm:p-4">
      <Link to={`/series/${item.seriesId}`} className="shrink-0">
        <Cover src={apiUrl(item.coverUrl)} alt={item.seriesTitle} className="aspect-[2/3] w-12 sm:w-14" />
      </Link>
      <div className="min-w-0 flex-1">
        <div className="mb-1 flex flex-wrap items-center gap-1.5">
          <Badge tone={chapter ? "info" : "accent"}>
            {chapter ? <BookOpen className="size-3" /> : <Sparkles className="size-3" />}
            {chapter ? t("New chapter") : t("New title")}
          </Badge>
          {item.language && <Badge>{item.language}</Badge>}
          {item.languages?.map((language) => <Badge key={language}>{language}</Badge>)}
          <span className="text-xs text-muted">{relative(item.at)}</span>
        </div>
        <Link to={`/series/${item.seriesId}`} className="font-medium hover:text-accent-2">{item.seriesTitle}</Link>
        {chapter && (
          <p className="truncate text-sm text-muted">{t("Chapter") + " "}{item.number}{item.title ? ` · ${item.title}` : ""}</p>
        )}
      </div>
      {chapter && item.readable ? (
        <Link to={`/read/${item.chapterId}`}><Button variant="primary" icon={<BookOpen className="size-4" />}>{t("Read")}</Button></Link>
      ) : !chapter ? (
        <Link to={`/series/${item.seriesId}`}><Button icon={<BookPlus className="size-4" />}>{t("View")}</Button></Link>
      ) : null}
    </div>
  );
}

function groupByDay(items: Update[]): [string, Update[]][] {
  const groups = new Map<string, Update[]>();
  for (const item of items) {
    const day = new Date(item.at).toLocaleDateString(undefined, { weekday: "long", year: "numeric", month: "long", day: "numeric" });
    groups.set(day, [...(groups.get(day) ?? []), item]);
  }
  return [...groups.entries()];
}
