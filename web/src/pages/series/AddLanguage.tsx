import { t } from "../../lib/i18n/core";
import { useState } from "react";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type S, type SourceManga } from "../../api/client";
import { Modal } from "../../components/ui";
import { LanguageSelect, useLanguageFolders, useLanguages } from "../../components/LanguageSelect";
import { languageName } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { SourceSearch, type PickGroup, type Scope } from "./SourceSearch";

/**
 * AddLanguageModal adds an edition of this title in another language: pick a
 * language, find the title at a source in it, and it lands in that
 * language's folder. A language the title already has gets the source instead.
 */
export function AddLanguageModal({ series, onClose }: { series: S["SeriesResource"]; onClose: () => void }) {
  const have = new Set((series.editions ?? []).map((e) => e.language));
  const langs = useLanguages();
  const [picked, setLang] = useState<string | null>(null);
  // until you pick, the first language this title doesn't have yet
  const lang = picked ?? langs.find((l) => !have.has(l)) ?? "";
  const titles = [...new Set([series.workTitle, series.title, ...(series.metadata.altTitles ?? [])].filter((x): x is string => !!x))];
  const [query, setQuery] = useState(series.workTitle || series.title);
  const [scope, setScope] = useState<Scope>("active");
  const [keys, setKeys] = useState<string[]>([]);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const folder = useLanguageFolders(lang ? [lang] : []).get(lang);
  const qc = useQueryClient();
  const toast = useToast();
  const nav = useNavigate();

  const add = async (m: SourceManga, g: PickGroup) => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await unwrap(
        api.POST("/api/v1/series/editions", {
          body: {
            workId: series.workId,
            sources: [{ moduleId: g.moduleId, sourceId: g.sourceId, url: m.url, engineRef: m.engineRef, title: m.title, sourceName: g.sourceName, lang }],
            monitor: "all",
            monitorNew: "all",
            searchMissing: true,
          },
        }),
      );
      qc.invalidateQueries({ queryKey: ["series"] });
      qc.invalidateQueries({ queryKey: ["rootfolders"] });
      const e = res.editions[0];
      toast.success(t("{lang} edition added", { lang: languageName(lang) }), t("Fetching chapters…"));
      onClose();
      if (e) nav(`/series/${e.id}`);
    } catch (err) {
      toast.fromError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open onClose={onClose} title={t("Add a language to {title}", { title: series.workTitle || series.title })} size="xl">
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1.5 text-sm font-medium">
          {t("Language")}
          <LanguageSelect value={lang} onChange={setLang} placeholder={t("Choose…")} className="w-44" />
        </label>
        {lang && (
          <div className="flex min-w-0 flex-col gap-1.5 text-sm">
            <span className="font-medium">{t("Saved to")}</span>
            {have.has(lang) ? (
              <span className="text-muted">{t("Joins the {lang} edition this title already has", { lang: languageName(lang) })}</span>
            ) : folder?.error ? (
              <span className="text-warn">{folder.error}</span>
            ) : folder ? (
              <span className="font-mono text-xs text-muted">
                {folder.path}
                {!folder.exists && " · " + t("new folder")}
              </span>
            ) : null}
          </div>
        )}
      </div>
      {lang && !folder?.error && (
        <SourceSearch
          query={query}
          setQuery={setQuery}
          titles={titles}
          scope={scope}
          setScope={setScope}
          lang={lang}
          setLang={setLang}
          keys={keys}
          setKeys={setKeys}
          more={more}
          setMore={setMore}
          selected={[]}
          onPick={(m, g) => void add(m, g)}
        />
      )}
      <p className="mt-4 text-xs text-muted">
        {t("The new edition uses this language's profile and reading direction from Settings → Search, and downloads every chapter.")}
      </p>
    </Modal>
  );
}
