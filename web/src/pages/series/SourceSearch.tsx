import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Search } from "lucide-react";
import { api, apiUrl, unwrap, type SearchGroup, type SourceManga } from "../../api/client";
import { useSources } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, ErrorBox, Input, Loading, Modal, Select } from "../../components/ui";

export type Picked = { manga: SourceManga; group: SearchGroup };

/** SourceResults searches catalogs and renders grouped results. */
export function SourceResults({
  query,
  lang,
  selected,
  onPick,
}: {
  query: string;
  lang: string;
  selected?: Picked[];
  onPick: (m: SourceManga, g: SearchGroup) => void;
}) {
  const { data, isFetching, error } = useQuery({
    queryKey: ["source-search", query, lang],
    queryFn: () => unwrap(api.GET("/api/v1/sources/search", { params: { query: { q: query, lang: lang || undefined } } })),
    enabled: query.trim().length > 0,
    staleTime: 5 * 60_000,
  });
  const isSelected = (m: SourceManga, g: SearchGroup) => selected?.some((p) => p.group.moduleId === g.moduleId && p.group.sourceId === g.sourceId && p.manga.url === m.url);
  if (!query) return null;
  if (isFetching) return <Loading />;
  if (error) return <ErrorBox error={error} />;
  const groups = (data ?? []).filter((g) => g.results.length > 0 || g.error);
  if (groups.length === 0) return <p className="text-sm text-muted">No results.</p>;
  return (
    <div className="flex flex-col gap-4">
      {groups.map((g) => (
        <div key={g.moduleId + ":" + g.sourceId}>
          <div className="mb-2 flex items-center gap-2 text-sm font-medium">
            {g.sourceName} <Badge>{g.lang}</Badge>
            {g.error && (
              <span className="truncate text-xs font-normal text-err" title={g.error}>
                {g.error}
              </span>
            )}
          </div>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(110px,1fr))] gap-3">
            {g.results.slice(0, 12).map((m) => {
              const sel = isSelected(m, g);
              return (
                <button
                  key={m.url}
                  type="button"
                  onClick={() => onPick(m, g)}
                  className={`group relative flex flex-col gap-1 rounded-md p-1 text-left ${sel ? "bg-accent/15 ring-2 ring-accent" : "hover:bg-panel-2"}`}
                >
                  <Cover
                    src={apiUrl(`api/v1/sources/${g.moduleId}/${g.sourceId}/thumbnail`, { url: m.url, engineRef: m.engineRef })}
                    alt={m.title}
                    className="aspect-[2/3] w-full"
                  />
                  {sel && (
                    <span className="absolute right-2 top-2 rounded-full bg-accent p-0.5 text-white">
                      <Check className="size-3.5" />
                    </span>
                  )}
                  <span className="line-clamp-2 text-xs">{m.title}</span>
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

export function SourceSearchBar({ query, setQuery, lang, setLang }: { query: string; setQuery: (q: string) => void; lang: string; setLang: (l: string) => void }) {
  const { data: sources } = useSources();
  const langs = useMemo(() => Array.from(new Set((sources ?? []).map((s) => s.lang))).sort(), [sources]);
  const [draft, setDraft] = useState(query);
  return (
    <form
      className="mb-4 flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        setQuery(draft.trim());
      }}
    >
      <Input value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Title to search at sources" />
      <Select className="w-32" value={lang} onChange={(e) => setLang(e.target.value)}>
        <option value="">all langs</option>
        {langs.map((l) => (
          <option key={l} value={l}>
            {l}
          </option>
        ))}
      </Select>
      <Button type="submit" variant="primary" icon={<Search className="size-4" />}>
        Search
      </Button>
    </form>
  );
}

export function SourceSearchModal({
  initialQuery,
  title,
  onPick,
  onClose,
}: {
  initialQuery: string;
  title: string;
  onPick: (m: SourceManga, g: SearchGroup) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState(initialQuery);
  const [lang, setLang] = useState("en");
  return (
    <Modal open onClose={onClose} title={title} size="xl">
      <SourceSearchBar query={query} setQuery={setQuery} lang={lang} setLang={setLang} />
      <SourceResults query={query} lang={lang} onPick={onPick} />
    </Modal>
  );
}
