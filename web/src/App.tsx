import { useEffect } from "react";
import { Navigate, Route, Routes } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Layout } from "./components/Layout";
import { Loading } from "./components/ui";
import { useAuthStatus } from "./api/queries";
import { onServerEvent, useLiveUpdates } from "./lib/events";
import { useToast } from "./lib/toast";
import { LoginPage } from "./pages/auth/Login";
import { SeriesIndex } from "./pages/series/SeriesIndex";
import { SeriesDetail } from "./pages/series/SeriesDetail";
import { AddOptionsStep, AddSearchStep, AddSourcesStep } from "./pages/series/AddSeries";
import { QueuePage } from "./pages/activity/Queue";
import { HistoryPage } from "./pages/activity/History";
import { BlocklistPage } from "./pages/activity/Blocklist";
import { WantedPage } from "./pages/activity/Wanted";
import { SourcesPage } from "./pages/sources/Sources";
import { SearchSettingsPage } from "./pages/settings/SearchSettings";
import { SchedulePage } from "./pages/settings/Schedule";
import { CleanupPage } from "./pages/settings/Cleanup";
import { ModulesPage } from "./pages/settings/Modules";
import { ProfilesPage } from "./pages/settings/Profiles";
import { MediaPage } from "./pages/settings/Media";
import { ReadersPage } from "./pages/settings/Readers";
import { ReadingAppsPage } from "./pages/settings/ReadingApps";
import { GeneralPage } from "./pages/settings/General";
import { DownloadsPage } from "./pages/settings/Downloads";
import { StatusPage } from "./pages/system/Status";
import { TasksPage } from "./pages/system/Tasks";
import { BackupsPage } from "./pages/system/Backups";
import { LogsPage } from "./pages/system/Logs";
import { ImportsPage } from "./pages/import/Imports";
import { ImportDetailPage } from "./pages/import/ImportDetail";

export function App() {
  const { data: auth, isLoading } = useAuthStatus();
  const qc = useQueryClient();
  const toast = useToast();
  const authed = !!auth?.authenticated;
  useLiveUpdates(authed);

  useEffect(() => {
    const onUnauth = () => qc.invalidateQueries({ queryKey: ["auth"] });
    window.addEventListener("mangarr:unauthorized", onUnauth);
    return () => window.removeEventListener("mangarr:unauthorized", onUnauth);
  }, [qc]);

  useEffect(() => {
    const off = onServerEvent((type, payload) => {
        const p = payload as { title?: string; message?: string; seriesTitle?: string; chapter?: string };
        if (type === "chapter.imported") toast.success(`${p.seriesTitle} ch. ${p.chapter} imported`);
        if (type === "download.failed") toast.error(p.title ?? "Download failed", p.message);
        if (type === "health.issue") toast.warning(p.title ?? "Health issue", p.message);
        if (type === "cleanup.done") toast.info(p.title ?? "Cleanup done", p.message);
    });
    return () => {
      off();
    };
  }, [toast]);

  if (isLoading) return <Loading />;
  if (!authed) return <LoginPage setup={!!auth?.needsSetup} />;

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<SeriesIndex />} />
        <Route path="series/:id" element={<SeriesDetail />} />
        <Route path="add" element={<AddSearchStep />} />
        <Route path="add/:moduleId/:metaId/sources" element={<AddSourcesStep />} />
        <Route path="add/:moduleId/:metaId/options" element={<AddOptionsStep />} />
        <Route path="import" element={<ImportsPage />} />
        <Route path="import/:id" element={<ImportDetailPage />} />
        <Route path="activity" element={<Navigate to="/activity/queue" replace />} />
        <Route path="activity/queue" element={<QueuePage />} />
        <Route path="activity/history" element={<HistoryPage />} />
        <Route path="activity/blocklist" element={<BlocklistPage />} />
        <Route path="wanted" element={<WantedPage />} />
        <Route path="sources" element={<SourcesPage />} />
        <Route path="sources/:tab" element={<SourcesPage />} />
        <Route path="cleanup" element={<CleanupPage />} />
        <Route path="settings" element={<Navigate to="/settings/media" replace />} />
        <Route path="settings/media" element={<MediaPage />} />
        <Route path="settings/profiles" element={<ProfilesPage />} />
        <Route path="settings/sources" element={<ModulesPage kind="source" />} />
        <Route path="settings/search" element={<SearchSettingsPage />} />
        <Route path="settings/schedule" element={<SchedulePage />} />
        <Route path="settings/metadata" element={<ModulesPage kind="metadata" />} />
        <Route path="settings/library" element={<ModulesPage kind="library" />} />
        <Route path="settings/notifications" element={<ModulesPage kind="notify" />} />
        <Route path="settings/upscalers" element={<ModulesPage kind="upscale" />} />
        <Route path="settings/readers" element={<ReadersPage />} />
        <Route path="settings/reading" element={<ReadingAppsPage />} />
        <Route path="settings/downloads" element={<DownloadsPage />} />
        <Route path="settings/general" element={<GeneralPage />} />
        <Route path="system" element={<Navigate to="/system/status" replace />} />
        <Route path="system/status" element={<StatusPage />} />
        <Route path="system/tasks" element={<TasksPage />} />
        <Route path="system/backups" element={<BackupsPage />} />
        <Route path="system/logs" element={<LogsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
