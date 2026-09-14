// Package model holds the persisted entities (bun models). Business logic
// lives in the service packages; this package only describes data.
package model

import (
	"time"

	"github.com/uptrace/bun"
)

// ---- Users / settings -------------------------------------------------------

type User struct {
	bun.BaseModel `bun:"table:users"`
	ID            int64     `bun:"id,pk,autoincrement" json:"id"`
	Username      string    `bun:"username,notnull" json:"username"`
	PasswordHash  string    `bun:"password_hash,notnull" json:"-"`
	CreatedAt     time.Time `bun:"created_at,notnull" json:"createdAt"`
}

type Setting struct {
	bun.BaseModel `bun:"table:settings"`
	Key           string    `bun:"key,pk"`
	Value         string    `bun:"value,notnull"`
	UpdatedAt     time.Time `bun:"updated_at,notnull"`
}

type Tag struct {
	bun.BaseModel `bun:"table:tags"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Label         string `bun:"label,notnull" json:"label"`
}

type RootFolder struct {
	bun.BaseModel `bun:"table:root_folders"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Path          string `bun:"path,notnull" json:"path"`
	Language      string `bun:"language,notnull" json:"language"`
	// ManagedBy is "env" when the folder comes from MANGARR_ROOT_FOLDERS.
	ManagedBy string    `bun:"managed_by,notnull" json:"managedBy,omitempty"`
	CreatedAt time.Time `bun:"created_at,notnull" json:"createdAt"`
}

// ---- Profiles ---------------------------------------------------------------

type Profile struct {
	bun.BaseModel `bun:"table:profiles"`
	ID            int64         `bun:"id,pk,autoincrement" json:"id"`
	Name          string        `bun:"name,notnull" json:"name"`
	IsDefault     bool          `bun:"is_default,notnull" json:"isDefault"`
	Config        ProfileConfig `bun:"config,notnull" json:"config"`
	CreatedAt     time.Time     `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt     time.Time     `bun:"updated_at,notnull" json:"updatedAt"`
}

type ProfileConfig struct {
	// PreferredScanlators are regexes; earlier entries rank higher.
	PreferredScanlators []string `json:"preferredScanlators"`
	// BlockedScanlators are regexes; matching releases are rejected.
	BlockedScanlators []string `json:"blockedScanlators"`
	// AllowUpgrades replaces an existing chapter when a better-ranked release appears.
	AllowUpgrades bool `json:"allowUpgrades"`
	// MinPages rejects chapters with fewer pages (0 = off).
	MinPages int `json:"minPages"`
	// Upscale settings applied to chapters downloaded with this profile.
	Upscale UpscaleConfig `json:"upscale"`
	// Cleanup overrides; nil fields inherit the global cleanup settings.
	Cleanup CleanupOverride `json:"cleanup"`
}

type UpscaleConfig struct {
	Enabled bool `json:"enabled"`
	// UpscalerID selects a specific upscale module instance; 0 = first enabled by priority.
	UpscalerID int64 `json:"upscalerId"`
	// MinWidth: pages narrower than this are upscaled.
	MinWidth int `json:"minWidth"`
	// MaxWidth caps output width (downscaled after upscaling). 0 = no cap.
	MaxWidth int `json:"maxWidth"`
	// Model name understood by the upscaler (e.g. "waifu2x-cunet", "realcugan", "realesr-animevideov3").
	Model string `json:"model"`
	// Noise reduction level (model dependent, -1..3).
	Noise int `json:"noise"`
	// Format of processed pages: "webp", "jpeg", "png".
	Format  string `json:"format"`
	Quality int    `json:"quality"`
}

type CleanupOverride struct {
	Enabled      *bool `json:"enabled,omitempty"`
	KeepLastRead *int  `json:"keepLastRead,omitempty"`
	GraceDays    *int  `json:"graceDays,omitempty"`
}

// ---- Modules (provider definitions) -----------------------------------------

type ProviderDefinition struct {
	bun.BaseModel  `bun:"table:provider_definitions"`
	ID             int64          `bun:"id,pk,autoincrement" json:"id"`
	Kind           string         `bun:"kind,notnull" json:"kind"`
	Implementation string         `bun:"implementation,notnull" json:"implementation"`
	Name           string         `bun:"name,notnull" json:"name"`
	Enabled        bool           `bun:"enabled,notnull" json:"enabled"`
	Priority       int            `bun:"priority,notnull" json:"priority"`
	Tags           []int64        `bun:"tags,notnull" json:"tags"`
	Events         []string       `bun:"events,notnull" json:"events"`
	Settings       map[string]any `bun:"settings,notnull" json:"settings"`
	// ManagedBy is "env:<NAME>" for instances defined by MANGARR_MODULE_<NAME>_* variables.
	ManagedBy string    `bun:"managed_by,notnull" json:"managedBy,omitempty"`
	CreatedAt time.Time `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}

// ---- Series -----------------------------------------------------------------

const (
	StatusUnknown   = "unknown"
	StatusOngoing   = "ongoing"
	StatusCompleted = "completed"
	StatusHiatus    = "hiatus"
	StatusCancelled = "cancelled"
)

type Series struct {
	bun.BaseModel       `bun:"table:series"`
	ID                  int64          `bun:"id,pk,autoincrement" json:"id"`
	Title               string         `bun:"title,notnull" json:"title"`
	SortTitle           string         `bun:"sort_title,notnull" json:"sortTitle"`
	Status              string         `bun:"status,notnull" json:"status"`
	Monitored           bool           `bun:"monitored,notnull" json:"monitored"`
	MonitorNew          string         `bun:"monitor_new,notnull" json:"monitorNew"` // "all" | "none"
	RootFolderID        int64          `bun:"root_folder_id,notnull" json:"rootFolderId"`
	Path                string         `bun:"path,notnull" json:"path"` // folder name under the root folder
	ProfileID           int64          `bun:"profile_id,notnull" json:"profileId"`
	Language            string         `bun:"language,notnull" json:"language"`
	ReadingDirection    string         `bun:"reading_direction,notnull" json:"readingDirection"` // rtl | ltr | vertical | webtoon
	Tags                []int64        `bun:"tags,notnull" json:"tags"`
	Metadata            SeriesMetadata `bun:"metadata,notnull" json:"metadata"`
	AddOptions          AddOptions     `bun:"add_options,notnull" json:"addOptions"`
	AddedAt             time.Time      `bun:"added_at,notnull" json:"addedAt"`
	UpdatedAt           time.Time      `bun:"updated_at,notnull" json:"updatedAt"`
	LastMetadataRefresh *time.Time     `bun:"last_metadata_refresh" json:"lastMetadataRefresh,omitempty"`
}

// Monitor options applied after the first chapter sync of a new series.
const (
	MonitorAll    = "all"
	MonitorFuture = "future"
	MonitorLatest = "latest"
	MonitorFrom   = "from"
	MonitorNone   = "none"
)

// AddOptions are applied on the first successful sync, then cleared (Pending=false).
type AddOptions struct {
	Pending       bool    `json:"pending"`
	Monitor       string  `json:"monitor,omitempty"`
	LatestCount   int     `json:"latestCount,omitempty"`
	FromChapter   float64 `json:"fromChapter,omitempty"`
	SearchMissing bool    `json:"searchMissing,omitempty"`
}

// SeriesMetadata is the merged metadata of a series plus provenance and locks.
type SeriesMetadata struct {
	AltTitles     []string          `json:"altTitles,omitempty"`
	Description   string            `json:"description,omitempty"`
	Year          int               `json:"year,omitempty"`
	Authors       []string          `json:"authors,omitempty"`
	Artists       []string          `json:"artists,omitempty"`
	Genres        []string          `json:"genres,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Publisher     string            `json:"publisher,omitempty"`
	CoverURL      string            `json:"coverUrl,omitempty"`
	Links         map[string]string `json:"links,omitempty"`       // label -> url
	ExternalIDs   map[string]string `json:"externalIds,omitempty"` // "anilist" -> "30013"
	AgeRating     string            `json:"ageRating,omitempty"`
	Format        string            `json:"format,omitempty"` // manga | manhwa | manhua | comic | oneshot
	TotalChapters int               `json:"totalChapters,omitempty"`
	// Provenance maps a field name to the module that supplied it ("anilist", "source", "user").
	Provenance map[string]string `json:"provenance,omitempty"`
	// Locks lists fields the user edited; refreshes do not overwrite them.
	Locks []string `json:"locks,omitempty"`
}

func (m SeriesMetadata) Locked(field string) bool {
	for _, l := range m.Locks {
		if l == field {
			return true
		}
	}
	return false
}

type SeriesSource struct {
	bun.BaseModel        `bun:"table:series_sources"`
	ID                   int64      `bun:"id,pk,autoincrement" json:"id"`
	SeriesID             int64      `bun:"series_id,notnull" json:"seriesId"`
	ModuleID             int64      `bun:"module_id,notnull" json:"moduleId"`
	SourceID             string     `bun:"source_id,notnull" json:"sourceId"`
	SourceName           string     `bun:"source_name,notnull" json:"sourceName"`
	Lang                 string     `bun:"lang,notnull" json:"lang"`
	MangaURL             string     `bun:"manga_url,notnull" json:"mangaUrl"`
	Title                string     `bun:"title,notnull" json:"title"`
	WebURL               string     `bun:"web_url,notnull" json:"webUrl"`
	EngineRef            string     `bun:"engine_ref,notnull" json:"-"`
	Priority             int        `bun:"priority,notnull" json:"priority"`
	Enabled              bool       `bun:"enabled,notnull" json:"enabled"`
	CheckIntervalMinutes int        `bun:"check_interval_minutes,notnull" json:"checkIntervalMinutes"` // 0 = automatic
	LastCheckedAt        *time.Time `bun:"last_checked_at" json:"lastCheckedAt,omitempty"`
	LastSuccessAt        *time.Time `bun:"last_success_at" json:"lastSuccessAt,omitempty"`
	NextCheckAt          time.Time  `bun:"next_check_at,notnull" json:"nextCheckAt"`
	ConsecutiveFailures  int        `bun:"consecutive_failures,notnull" json:"consecutiveFailures"`
	BackoffUntil         *time.Time `bun:"backoff_until" json:"backoffUntil,omitempty"`
	LastError            string     `bun:"last_error,notnull" json:"lastError"`
	CreatedAt            time.Time  `bun:"created_at,notnull" json:"createdAt"`
}

// ---- Chapters ---------------------------------------------------------------

const (
	ChapterMissing     = "missing"
	ChapterQueued      = "queued"
	ChapterDownloading = "downloading"
	ChapterProcessing  = "processing"
	ChapterImported    = "imported"
	ChapterCleaned     = "cleaned"
	ChapterFailed      = "failed"
)

type Chapter struct {
	bun.BaseModel `bun:"table:chapters"`
	ID            int64      `bun:"id,pk,autoincrement" json:"id"`
	SeriesID      int64      `bun:"series_id,notnull" json:"seriesId"`
	NumberKey     string     `bun:"number_key,notnull" json:"number"`
	NumberSort    float64    `bun:"number_sort,notnull" json:"numberSort"`
	Volume        string     `bun:"volume,notnull" json:"volume"`
	Title         string     `bun:"title,notnull" json:"title"`
	Monitored     bool       `bun:"monitored,notnull" json:"monitored"`
	State         string     `bun:"state,notnull" json:"state"`
	FileID        *int64     `bun:"file_id" json:"fileId,omitempty"`
	CleanedAt     *time.Time `bun:"cleaned_at" json:"cleanedAt,omitempty"`
	ReleaseDate   *time.Time `bun:"release_date" json:"releaseDate,omitempty"`
	FirstSeenAt   time.Time  `bun:"first_seen_at,notnull" json:"firstSeenAt"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull" json:"updatedAt"`
}

type ChapterRelease struct {
	bun.BaseModel  `bun:"table:chapter_releases"`
	ID             int64      `bun:"id,pk,autoincrement" json:"id"`
	SeriesID       int64      `bun:"series_id,notnull" json:"seriesId"`
	ChapterID      *int64     `bun:"chapter_id" json:"chapterId,omitempty"` // nil = unparsed number, needs manual mapping
	SeriesSourceID int64      `bun:"series_source_id,notnull" json:"seriesSourceId"`
	ChapterURL     string     `bun:"chapter_url,notnull" json:"chapterUrl"`
	WebURL         string     `bun:"web_url,notnull" json:"webUrl"`
	EngineRef      string     `bun:"engine_ref,notnull" json:"-"`
	Name           string     `bun:"name,notnull" json:"name"`
	Scanlator      string     `bun:"scanlator,notnull" json:"scanlator"`
	RawNumber      float64    `bun:"raw_number,notnull" json:"rawNumber"`
	UploadDate     *time.Time `bun:"upload_date" json:"uploadDate,omitempty"`
	Removed        bool       `bun:"removed,notnull" json:"removed"`
	CreatedAt      time.Time  `bun:"created_at,notnull" json:"createdAt"`
}

type ChapterFile struct {
	bun.BaseModel `bun:"table:chapter_files"`
	ID            int64     `bun:"id,pk,autoincrement" json:"id"`
	ChapterID     int64     `bun:"chapter_id,notnull" json:"chapterId"`
	SeriesID      int64     `bun:"series_id,notnull" json:"seriesId"`
	RelativePath  string    `bun:"relative_path,notnull" json:"relativePath"`
	Size          int64     `bun:"size,notnull" json:"size"`
	PageCount     int       `bun:"page_count,notnull" json:"pageCount"`
	AvgWidth      int       `bun:"avg_width,notnull" json:"avgWidth"`
	Format        string    `bun:"format,notnull" json:"format"`
	ReleaseID     *int64    `bun:"release_id" json:"releaseId,omitempty"`
	Scanlator     string    `bun:"scanlator,notnull" json:"scanlator"`
	SourceName    string    `bun:"source_name,notnull" json:"sourceName"`
	SHA256        string    `bun:"sha256,notnull" json:"sha256"`
	Upscaled      bool      `bun:"upscaled,notnull" json:"upscaled"`
	UpscaleModel  string    `bun:"upscale_model,notnull" json:"upscaleModel"`
	SizeBefore    int64     `bun:"size_before,notnull" json:"sizeBefore"`
	ImportedAt    time.Time `bun:"imported_at,notnull" json:"importedAt"`
}

// ---- Queue / history / blocklist ---------------------------------------------

const (
	JobQueued      = "queued"
	JobDownloading = "downloading"
	JobProcessing  = "processing"
	JobImporting   = "importing"
	JobCompleted   = "completed"
	JobFailed      = "failed"

	JobKindDownload  = "download"
	JobKindReprocess = "reprocess" // re-run processing (e.g. upscale) on an existing file
)

type DownloadJob struct {
	bun.BaseModel `bun:"table:download_jobs"`
	ID            int64      `bun:"id,pk,autoincrement" json:"id"`
	Kind          string     `bun:"kind,notnull" json:"kind"`
	SeriesID      int64      `bun:"series_id,notnull" json:"seriesId"`
	ChapterID     int64      `bun:"chapter_id,notnull" json:"chapterId"`
	ReleaseID     *int64     `bun:"release_id" json:"releaseId,omitempty"`
	Status        string     `bun:"status,notnull" json:"status"`
	Progress      int        `bun:"progress,notnull" json:"progress"`
	PagesDone     int        `bun:"pages_done,notnull" json:"pagesDone"`
	PagesTotal    int        `bun:"pages_total,notnull" json:"pagesTotal"`
	Attempt       int        `bun:"attempt,notnull" json:"attempt"`
	IsUpgrade     bool       `bun:"is_upgrade,notnull" json:"isUpgrade"`
	Error         string     `bun:"error,notnull" json:"error"`
	NotBefore     time.Time  `bun:"not_before,notnull" json:"notBefore"`
	CreatedAt     time.Time  `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull" json:"updatedAt"`
	StartedAt     *time.Time `bun:"started_at" json:"startedAt,omitempty"`
}

const (
	HistoryFound     = "found"
	HistoryGrabbed   = "grabbed"
	HistoryImported  = "imported"
	HistoryUpgraded  = "upgraded"
	HistoryFailed    = "failed"
	HistoryDeleted   = "deleted"
	HistoryCleaned   = "cleaned"
	HistoryRestored  = "restored"
	HistoryUpscaled  = "upscaled"
	HistoryUnparsed  = "unparsed"
	HistoryRetitled  = "renamed"
	HistoryBlocklist = "blocklisted"
)

type History struct {
	bun.BaseModel `bun:"table:history"`
	ID            int64             `bun:"id,pk,autoincrement" json:"id"`
	SeriesID      int64             `bun:"series_id,notnull" json:"seriesId"`
	ChapterID     *int64            `bun:"chapter_id" json:"chapterId,omitempty"`
	EventType     string            `bun:"event_type,notnull" json:"eventType"`
	SourceTitle   string            `bun:"source_title,notnull" json:"sourceTitle"`
	Data          map[string]string `bun:"data,notnull" json:"data"`
	CreatedAt     time.Time         `bun:"created_at,notnull" json:"createdAt"`
}

type Blocklist struct {
	bun.BaseModel  `bun:"table:blocklist"`
	ID             int64     `bun:"id,pk,autoincrement" json:"id"`
	SeriesID       int64     `bun:"series_id,notnull" json:"seriesId"`
	ChapterID      *int64    `bun:"chapter_id" json:"chapterId,omitempty"`
	SeriesSourceID int64     `bun:"series_source_id,notnull" json:"seriesSourceId"`
	ChapterURL     string    `bun:"chapter_url,notnull" json:"chapterUrl"`
	Scanlator      string    `bun:"scanlator,notnull" json:"scanlator"`
	Reason         string    `bun:"reason,notnull" json:"reason"`
	CreatedAt      time.Time `bun:"created_at,notnull" json:"createdAt"`
}

// ---- Commands / tasks --------------------------------------------------------

const (
	CommandQueued    = "queued"
	CommandStarted   = "started"
	CommandCompleted = "completed"
	CommandFailed    = "failed"
	CommandOrphaned  = "orphaned"
)

type Command struct {
	bun.BaseModel `bun:"table:commands"`
	ID            int64          `bun:"id,pk,autoincrement" json:"id"`
	Name          string         `bun:"name,notnull" json:"name"`
	Body          map[string]any `bun:"body,notnull" json:"body"`
	Status        string         `bun:"status,notnull" json:"status"`
	Trigger       string         `bun:"trigger,notnull" json:"trigger"`
	Message       string         `bun:"message,notnull" json:"message"`
	QueuedAt      time.Time      `bun:"queued_at,notnull" json:"queuedAt"`
	StartedAt     *time.Time     `bun:"started_at" json:"startedAt,omitempty"`
	EndedAt       *time.Time     `bun:"ended_at" json:"endedAt,omitempty"`
	DurationMs    int64          `bun:"duration_ms,notnull" json:"durationMs"`
	Error         string         `bun:"error,notnull" json:"error"`
}

type ScheduledTask struct {
	bun.BaseModel   `bun:"table:scheduled_tasks"`
	Name            string     `bun:"name,pk" json:"name"`
	IntervalMinutes int        `bun:"interval_minutes,notnull" json:"intervalMinutes"`
	LastExecution   *time.Time `bun:"last_execution" json:"lastExecution,omitempty"`
	LastStart       *time.Time `bun:"last_start" json:"lastStart,omitempty"`
}

// ---- Readers / progress ------------------------------------------------------

type Reader struct {
	bun.BaseModel   `bun:"table:readers"`
	ID              int64     `bun:"id,pk,autoincrement" json:"id"`
	Name            string    `bun:"name,notnull" json:"name"`
	CountForCleanup bool      `bun:"count_for_cleanup,notnull" json:"countForCleanup"`
	CreatedAt       time.Time `bun:"created_at,notnull" json:"createdAt"`
}

type ReaderAccount struct {
	bun.BaseModel `bun:"table:reader_accounts"`
	ID            int64             `bun:"id,pk,autoincrement" json:"id"`
	ReaderID      int64             `bun:"reader_id,notnull" json:"readerId"`
	ModuleID      int64             `bun:"module_id,notnull" json:"moduleId"`
	Credentials   map[string]string `bun:"credentials,notnull" json:"-"`
	ExternalUser  string            `bun:"external_user,notnull" json:"externalUser"`
	LastSyncAt    *time.Time        `bun:"last_sync_at" json:"lastSyncAt,omitempty"`
	LastError     string            `bun:"last_error,notnull" json:"lastError"`
	CreatedAt     time.Time         `bun:"created_at,notnull" json:"createdAt"`
}

type ChapterReadState struct {
	bun.BaseModel `bun:"table:chapter_read_states"`
	ID            int64      `bun:"id,pk,autoincrement" json:"id"`
	ReaderID      int64      `bun:"reader_id,notnull" json:"readerId"`
	ChapterID     int64      `bun:"chapter_id,notnull" json:"chapterId"`
	SeriesID      int64      `bun:"series_id,notnull" json:"seriesId"`
	Completed     bool       `bun:"completed,notnull" json:"completed"`
	Page          int        `bun:"page,notnull" json:"page"`
	ReadAt        *time.Time `bun:"read_at" json:"readAt,omitempty"`
	SyncedAt      time.Time  `bun:"synced_at,notnull" json:"syncedAt"`
}
