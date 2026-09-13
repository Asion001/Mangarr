// Package suwayomi implements a source module backed by a headless
// Suwayomi-Server, which runs Tachiyomi/Keiyoushi extensions (and handles
// Cloudflare through FlareSolverr). mangarr treats Suwayomi as a mostly
// stateless engine: identities are (sourceId, url) and Suwayomi's integer
// ids are only cached as EngineRef.
package suwayomi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
)

// PinnedVersion is the Suwayomi version the operations are validated against.
const PinnedVersion = "v2.3.2243"

// DefaultStore is the Keiyoushi extension repository.
const DefaultStore = "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json"

type Settings struct {
	URL      string `json:"url" label:"Suwayomi URL" type:"url" required:"true" placeholder:"http://suwayomi:4567" order:"1" help:"Address of your Suwayomi-Server (no need to publish its port outside Docker)."`
	Username string `json:"username" label:"Username" order:"2" advanced:"true" help:"Only when Suwayomi uses basic auth."`
	Password string `json:"password" label:"Password" secret:"true" order:"3" advanced:"true"`

	ExtensionStores []string `json:"extensionStores" label:"Extension stores" type:"tags" order:"4" help:"Extension repository index URLs. Keiyoushi is added by default."`

	AutoUpdateExtensions bool `json:"autoUpdateExtensions" label:"Auto-update extensions" order:"6" help:"Install extension updates automatically (sites change often; updates keep sources working)."`

	ManageSettings bool `json:"manageSettings" label:"Manage Suwayomi settings" order:"5" help:"Turn off Suwayomi's own library updater and auto-download (mangarr schedules everything) and apply the FlareSolverr settings below."`

	FlareSolverrEnabled          bool   `json:"flareSolverrEnabled" label:"Use FlareSolverr" order:"10" help:"Solve Cloudflare challenges through FlareSolverr/Byparr."`
	FlareSolverrURL              string `json:"flareSolverrUrl" label:"FlareSolverr URL" type:"url" placeholder:"http://flaresolverr:8191" order:"11"`
	FlareSolverrTimeout          int    `json:"flareSolverrTimeout" label:"FlareSolverr timeout (s)" order:"12" advanced:"true"`
	FlareSolverrSessionTTL       int    `json:"flareSolverrSessionTtl" label:"FlareSolverr session TTL (min)" order:"13" advanced:"true"`
	FlareSolverrResponseFallback bool   `json:"flareSolverrResponseFallback" label:"Use FlareSolverr response as fallback" order:"14" advanced:"true"`

	RequestTimeout int `json:"requestTimeout" label:"Request timeout (s)" order:"20" advanced:"true" help:"Timeout for a single Suwayomi request (sources can be slow)."`
}

func (s *Settings) Validate() error {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid Suwayomi URL %q", s.URL)
	}
	if s.FlareSolverrEnabled && s.FlareSolverrURL == "" {
		return errors.New("FlareSolverr URL is required when FlareSolverr is enabled")
	}
	return nil
}

func init() {
	modules.Register(&modules.Implementation{
		Kind:        modules.KindSource,
		Name:        "suwayomi",
		DisplayName: "Suwayomi (Keiyoushi extensions)",
		Description: "Runs Tachiyomi/Mihon extensions (e.g. Keiyoushi) through a headless Suwayomi-Server. Supports FlareSolverr for Cloudflare.",
		InfoURL:     "https://github.com/Suwayomi/Suwayomi-Server",
		Settings: func() any {
			return &Settings{
				URL: "http://suwayomi:4567", ExtensionStores: []string{DefaultStore}, ManageSettings: true, AutoUpdateExtensions: true,
				FlareSolverrTimeout: 60, FlareSolverrSessionTTL: 15, RequestTimeout: 180,
			}
		},
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			return New(deps, s.(*Settings))
		},
	})
}

type Module struct {
	s   *Settings
	c   *client
	log *slog.Logger

	mu        sync.Mutex
	appliedAt time.Time
}

func New(deps modules.Deps, s *Settings) (*Module, error) {
	base, err := url.Parse(strings.TrimRight(s.URL, "/"))
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(s.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	hc := &http.Client{Timeout: timeout}
	if deps.HTTP != nil {
		hc.Transport = deps.HTTP.Transport
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	return &Module{s: s, c: &client{base: base, http: hc, username: s.Username, password: s.Password}, log: log}, nil
}

// ---- lifecycle ---------------------------------------------------------------

type About struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	BuildType string `json:"buildType"`
}

func (m *Module) About(ctx context.Context) (*About, error) {
	var out struct {
		AboutServer About `json:"aboutServer"`
	}
	if err := m.c.do(ctx, opAbout, nil, &out); err != nil {
		return nil, err
	}
	return &out.AboutServer, nil
}

func (m *Module) Test(ctx context.Context) error {
	a, err := m.About(ctx)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(a.Name), "suwayomi") && !strings.Contains(strings.ToLower(a.Name), "tachidesk") {
		return fmt.Errorf("unexpected server %q", a.Name)
	}
	m.mu.Lock()
	m.appliedAt = time.Time{} // force re-apply
	m.mu.Unlock()
	return m.ensure(ctx)
}

// ensure applies managed settings and extension stores (at most hourly).
func (m *Module) ensure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.appliedAt) < time.Hour {
		return nil
	}
	if m.s.ManageSettings {
		settings := map[string]any{
			"globalUpdateInterval":           0,
			"autoDownloadNewChapters":        false,
			"flareSolverrEnabled":            m.s.FlareSolverrEnabled,
			"flareSolverrAsResponseFallback": m.s.FlareSolverrResponseFallback,
		}
		if m.s.FlareSolverrURL != "" {
			settings["flareSolverrUrl"] = m.s.FlareSolverrURL
		}
		if m.s.FlareSolverrTimeout > 0 {
			settings["flareSolverrTimeout"] = m.s.FlareSolverrTimeout
		}
		if m.s.FlareSolverrSessionTTL > 0 {
			settings["flareSolverrSessionTtl"] = m.s.FlareSolverrSessionTTL
		}
		if err := m.c.do(ctx, opSetSettings, map[string]any{"settings": settings}, nil); err != nil {
			return fmt.Errorf("apply settings: %w", err)
		}
	}
	for _, st := range m.s.ExtensionStores {
		st = strings.TrimSpace(st)
		if st == "" {
			continue
		}
		if err := m.c.do(ctx, opAddStore, map[string]any{"url": st}, nil); err != nil {
			var ge *GraphQLError
			if !errors.As(err, &ge) || !ge.Contains("exist") {
				return fmt.Errorf("add extension store %s: %w", st, err)
			}
		}
	}
	m.appliedAt = time.Now()
	return nil
}

func (m *Module) ensureQuiet(ctx context.Context) {
	if err := m.ensure(ctx); err != nil {
		m.log.Warn("suwayomi: applying settings failed", "err", err)
	}
}

// ---- catalogs ------------------------------------------------------------------

type gqlSource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Lang           string `json:"lang"`
	DisplayName    string `json:"displayName"`
	SupportsLatest bool   `json:"supportsLatest"`
	ContentWarning string `json:"contentWarning"`
	IconURL        string `json:"iconUrl"`
	Extension      struct {
		PkgName string `json:"pkgName"`
	} `json:"extension"`
}

func (m *Module) Sources(ctx context.Context) ([]source.SourceInfo, error) {
	m.ensureQuiet(ctx)
	var out struct {
		Sources struct {
			Nodes []gqlSource `json:"nodes"`
		} `json:"sources"`
	}
	if err := m.c.do(ctx, opSources, nil, &out); err != nil {
		return nil, err
	}
	res := make([]source.SourceInfo, 0, len(out.Sources.Nodes))
	for _, s := range out.Sources.Nodes {
		if s.ID == "0" { // Suwayomi's "Local source"
			continue
		}
		res = append(res, source.SourceInfo{ID: s.ID, Name: s.Name, Lang: s.Lang, DisplayName: s.DisplayName,
			SupportsLatest: s.SupportsLatest, NSFW: s.ContentWarning == "NSFW", IconURL: m.absURL(s.IconURL), Extension: s.Extension.PkgName})
	}
	return res, nil
}

type gqlManga struct {
	ID           int      `json:"id"`
	SourceID     string   `json:"sourceId"`
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	ThumbnailURL string   `json:"thumbnailUrl"`
	Author       string   `json:"author"`
	Artist       string   `json:"artist"`
	Description  string   `json:"description"`
	Genre        []string `json:"genre"`
	Status       string   `json:"status"`
	RealURL      string   `json:"realUrl"`
}

func (g gqlManga) toManga() source.Manga {
	return source.Manga{
		MangaRef:     source.MangaRef{SourceID: g.SourceID, URL: g.URL, EngineRef: strconv.Itoa(g.ID)},
		Title:        g.Title,
		ThumbnailURL: g.ThumbnailURL,
	}
}

func (m *Module) browse(ctx context.Context, sourceID, typ, query string, page int) (*source.MangaPage, error) {
	if page < 1 {
		page = 1
	}
	vars := map[string]any{"source": sourceID, "type": typ, "page": page}
	if query != "" {
		vars["query"] = query
	}
	var out struct {
		FetchSourceManga struct {
			HasNextPage bool       `json:"hasNextPage"`
			Mangas      []gqlManga `json:"mangas"`
		} `json:"fetchSourceManga"`
	}
	if err := m.c.do(ctx, opFetchSourceManga, vars, &out); err != nil {
		return nil, err
	}
	res := &source.MangaPage{HasNext: out.FetchSourceManga.HasNextPage, Mangas: []source.Manga{}}
	for _, g := range out.FetchSourceManga.Mangas {
		res.Mangas = append(res.Mangas, g.toManga())
	}
	return res, nil
}

func (m *Module) Search(ctx context.Context, sourceID, query string, page int) (*source.MangaPage, error) {
	m.ensureQuiet(ctx)
	return m.browse(ctx, sourceID, "SEARCH", query, page)
}

func (m *Module) Latest(ctx context.Context, sourceID string, page int) (*source.MangaPage, error) {
	return m.browse(ctx, sourceID, "LATEST", "", page)
}

func (m *Module) Popular(ctx context.Context, sourceID string, page int) (*source.MangaPage, error) {
	return m.browse(ctx, sourceID, "POPULAR", "", page)
}

// resolveManga returns the Suwayomi id for ref, re-linking when the cached id is stale.
func (m *Module) resolveManga(ctx context.Context, ref source.MangaRef, skipCached bool) (int, error) {
	if !skipCached {
		if id, err := strconv.Atoi(ref.EngineRef); err == nil && id > 0 {
			return id, nil
		}
	}
	var out struct {
		Mangas struct {
			Nodes []struct {
				ID int `json:"id"`
			} `json:"nodes"`
		} `json:"mangas"`
	}
	if err := m.c.do(ctx, opFindManga, map[string]any{"sourceId": ref.SourceID, "url": ref.URL}, &out); err != nil {
		return 0, err
	}
	if len(out.Mangas.Nodes) > 0 {
		return out.Mangas.Nodes[0].ID, nil
	}
	// Suwayomi only knows manga it has seen in a listing: search by title.
	if ref.TitleHint != "" {
		for page := 1; page <= 3; page++ {
			res, err := m.browse(ctx, ref.SourceID, "SEARCH", ref.TitleHint, page)
			if err != nil {
				return 0, err
			}
			for _, mg := range res.Mangas {
				if mg.URL == ref.URL {
					return strconv.Atoi(mg.EngineRef)
				}
			}
			if !res.HasNext {
				break
			}
		}
	}
	return 0, fmt.Errorf("%w: manga %s on source %s (search the source again to re-link)", source.ErrNotFound, ref.URL, ref.SourceID)
}

func isStale(err error) bool {
	var ge *GraphQLError
	return errors.As(err, &ge) && (ge.Contains("Collection is empty") || ge.Contains("NoSuchElement") || ge.Contains("not found"))
}

type gqlChapter struct {
	ID            int     `json:"id"`
	URL           string  `json:"url"`
	Name          string  `json:"name"`
	ChapterNumber float64 `json:"chapterNumber"`
	Scanlator     string  `json:"scanlator"`
	UploadDate    string  `json:"uploadDate"`
	SourceOrder   int     `json:"sourceOrder"`
	RealURL       string  `json:"realUrl"`
}

func (m *Module) Manga(ctx context.Context, ref source.MangaRef, withChapters bool) (*source.MangaDetails, []source.Chapter, error) {
	m.ensureQuiet(ctx)
	id, err := m.resolveManga(ctx, ref, false)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		FetchMangaAndChapters *struct {
			Manga    gqlManga     `json:"manga"`
			Chapters []gqlChapter `json:"chapters"`
		} `json:"fetchMangaAndChapters"`
	}
	vars := map[string]any{"id": id, "chapters": withChapters}
	err = m.c.do(ctx, opFetchMangaAndChapters, vars, &out)
	if err != nil && isStale(err) {
		if id, err = m.resolveManga(ctx, ref, true); err != nil {
			return nil, nil, err
		}
		vars["id"] = id
		out.FetchMangaAndChapters = nil
		err = m.c.do(ctx, opFetchMangaAndChapters, vars, &out)
	}
	var ge *GraphQLError
	noChapters := errors.As(err, &ge) && ge.Contains("No chapters found")
	if err != nil && !noChapters {
		return nil, nil, err
	}
	if out.FetchMangaAndChapters == nil {
		if noChapters {
			// Some sources throw when a manga has no chapters; the manga itself is fine.
			return &source.MangaDetails{Manga: source.Manga{MangaRef: source.MangaRef{SourceID: ref.SourceID, URL: ref.URL, EngineRef: strconv.Itoa(id)}}, Status: source.StatusUnknown}, []source.Chapter{}, nil
		}
		return nil, nil, fmt.Errorf("suwayomi returned no data for manga %d", id)
	}
	g := out.FetchMangaAndChapters.Manga
	det := &source.MangaDetails{
		Manga: g.toManga(), Author: g.Author, Artist: g.Artist, Description: strings.TrimSpace(g.Description),
		Genres: g.Genre, Status: mapStatus(g.Status), WebURL: g.RealURL,
	}
	det.ThumbnailURL = g.ThumbnailURL
	chapters := make([]source.Chapter, 0, len(out.FetchMangaAndChapters.Chapters))
	for _, c := range out.FetchMangaAndChapters.Chapters {
		ch := source.Chapter{URL: c.URL, EngineRef: strconv.Itoa(c.ID), Name: c.Name, Scanlator: strings.TrimSpace(c.Scanlator),
			Number: c.ChapterNumber, WebURL: c.RealURL}
		if ms, err := strconv.ParseInt(c.UploadDate, 10, 64); err == nil && ms > 0 {
			t := time.UnixMilli(ms).UTC()
			ch.UploadDate = &t
		}
		chapters = append(chapters, ch)
	}
	return det, chapters, nil
}

func mapStatus(s string) string {
	switch s {
	case "ONGOING":
		return source.StatusOngoing
	case "COMPLETED", "PUBLISHING_FINISHED":
		return source.StatusCompleted
	case "CANCELLED":
		return source.StatusCancelled
	case "ON_HIATUS":
		return source.StatusHiatus
	default:
		return source.StatusUnknown
	}
}

func (m *Module) resolveChapter(ctx context.Context, ref source.ChapterRef) (int, error) {
	mangaID, err := m.resolveManga(ctx, ref.Manga, false)
	if err != nil {
		return 0, err
	}
	find := func(mid int) (int, error) {
		var out struct {
			Chapters struct {
				Nodes []struct {
					ID int `json:"id"`
				} `json:"nodes"`
			} `json:"chapters"`
		}
		if err := m.c.do(ctx, opFindChapter, map[string]any{"mangaId": mid, "url": ref.URL}, &out); err != nil {
			return 0, err
		}
		if len(out.Chapters.Nodes) == 0 {
			return 0, nil
		}
		return out.Chapters.Nodes[0].ID, nil
	}
	if id, err := find(mangaID); err != nil || id > 0 {
		return id, err
	}
	// Unknown chapter: refresh the chapter list and try again.
	if _, _, err := m.Manga(ctx, ref.Manga, true); err != nil {
		return 0, err
	}
	if mangaID, err = m.resolveManga(ctx, ref.Manga, true); err != nil {
		return 0, err
	}
	id, err := find(mangaID)
	if err == nil && id == 0 {
		err = fmt.Errorf("%w: chapter %s", source.ErrNotFound, ref.URL)
	}
	return id, err
}

func (m *Module) Pages(ctx context.Context, ref source.ChapterRef) ([]source.Page, error) {
	id, err := strconv.Atoi(ref.EngineRef)
	if err != nil || id <= 0 {
		if id, err = m.resolveChapter(ctx, ref); err != nil {
			return nil, err
		}
	}
	var out struct {
		FetchChapterPages *struct {
			Pages []string `json:"pages"`
		} `json:"fetchChapterPages"`
	}
	err = m.c.do(ctx, opFetchChapterPages, map[string]any{"chapterId": id}, &out)
	if err != nil && isStale(err) {
		if id, err = m.resolveChapter(ctx, ref); err != nil {
			return nil, err
		}
		out.FetchChapterPages = nil
		err = m.c.do(ctx, opFetchChapterPages, map[string]any{"chapterId": id}, &out)
	}
	if err != nil {
		return nil, err
	}
	if out.FetchChapterPages == nil || len(out.FetchChapterPages.Pages) == 0 {
		return nil, errors.New("source returned no pages")
	}
	pages := make([]source.Page, len(out.FetchChapterPages.Pages))
	for i, p := range out.FetchChapterPages.Pages {
		pages[i] = source.Page{Index: i, URL: p}
	}
	return pages, nil
}

func (m *Module) FetchPage(ctx context.Context, p source.Page) (io.ReadCloser, string, error) {
	resp, err := m.c.get(ctx, p.URL)
	if err != nil {
		return nil, "", err
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

func (m *Module) Thumbnail(ctx context.Context, ref source.MangaRef) (io.ReadCloser, string, error) {
	id, err := m.resolveManga(ctx, ref, false)
	if err != nil {
		return nil, "", err
	}
	resp, err := m.c.get(ctx, fmt.Sprintf("/api/v1/manga/%d/thumbnail", id))
	if err != nil {
		return nil, "", err
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

func (m *Module) Maintain(ctx context.Context) error {
	return m.c.do(ctx, opClearCache, nil, nil)
}

// absURL keeps Suwayomi-relative paths relative: the API serves them through
// the module asset proxy because browsers usually can't reach Suwayomi.
func (m *Module) absURL(p string) string { return p }

// FetchAsset fetches a server-relative asset (icons, thumbnails).
func (m *Module) FetchAsset(ctx context.Context, path string) (io.ReadCloser, string, error) {
	if !strings.HasPrefix(path, "/api/v1/") {
		return nil, "", fmt.Errorf("asset path not allowed: %s", path)
	}
	resp, err := m.c.get(ctx, path)
	if err != nil {
		return nil, "", err
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// ---- extensions ------------------------------------------------------------------

type gqlExtension struct {
	PkgName         string `json:"pkgName"`
	Name            string `json:"name"`
	Lang            string `json:"lang"`
	VersionName     string `json:"versionName"`
	VersionCodeLong string `json:"versionCodeLong"`
	IsInstalled     bool   `json:"isInstalled"`
	HasUpdate       bool   `json:"hasUpdate"`
	IsObsolete      bool   `json:"isObsolete"`
	ContentWarning  string `json:"contentWarning"`
	IconURL         string `json:"iconUrl"`
}

func (m *Module) Extensions(ctx context.Context, refresh bool) ([]source.Extension, error) {
	m.ensureQuiet(ctx)
	var list []gqlExtension
	if refresh {
		var out struct {
			FetchExtensions struct {
				Extensions []gqlExtension `json:"extensions"`
			} `json:"fetchExtensions"`
		}
		if err := m.c.do(ctx, opFetchExtensions, nil, &out); err != nil {
			return nil, err
		}
		list = out.FetchExtensions.Extensions
	} else {
		var out struct {
			Extensions struct {
				Nodes []gqlExtension `json:"nodes"`
			} `json:"extensions"`
		}
		if err := m.c.do(ctx, opExtensions, nil, &out); err != nil {
			return nil, err
		}
		list = out.Extensions.Nodes
		if len(list) == 0 {
			return m.Extensions(ctx, true)
		}
	}
	res := make([]source.Extension, 0, len(list))
	for _, e := range list {
		code, _ := strconv.Atoi(e.VersionCodeLong)
		res = append(res, source.Extension{Pkg: e.PkgName, Name: e.Name, Lang: e.Lang, VersionName: e.VersionName, VersionCode: code,
			Installed: e.IsInstalled, HasUpdate: e.HasUpdate, Obsolete: e.IsObsolete, NSFW: e.ContentWarning == "NSFW", IconURL: m.absURL(e.IconURL)})
	}
	return res, nil
}

func (m *Module) patchExtension(ctx context.Context, pkg, action string) error {
	return m.c.do(ctx, opUpdateExtension, map[string]any{"id": pkg, action: true}, nil)
}

func (m *Module) InstallExtension(ctx context.Context, pkg string) error {
	return m.patchExtension(ctx, pkg, "install")
}
func (m *Module) UpdateExtension(ctx context.Context, pkg string) error {
	return m.patchExtension(ctx, pkg, "update")
}
func (m *Module) UninstallExtension(ctx context.Context, pkg string) error {
	return m.patchExtension(ctx, pkg, "uninstall")
}

func (m *Module) Stores(ctx context.Context) ([]string, error) {
	var out struct {
		ExtensionStores struct {
			Nodes []struct {
				IndexURL string `json:"indexUrl"`
			} `json:"nodes"`
		} `json:"extensionStores"`
	}
	if err := m.c.do(ctx, opStores, nil, &out); err != nil {
		return nil, err
	}
	res := []string{}
	for _, n := range out.ExtensionStores.Nodes {
		res = append(res, n.IndexURL)
	}
	return res, nil
}

func (m *Module) AddStore(ctx context.Context, u string) error {
	return m.c.do(ctx, opAddStore, map[string]any{"url": u}, nil)
}

func (m *Module) RemoveStore(ctx context.Context, u string) error {
	return m.c.do(ctx, opRemoveStore, map[string]any{"url": u}, nil)
}

// ---- source preferences --------------------------------------------------------

func (m *Module) SourcePreferences(ctx context.Context, sourceID string) ([]source.Preference, error) {
	var out struct {
		Source struct {
			Preferences []map[string]any `json:"preferences"`
		} `json:"source"`
	}
	if err := m.c.do(ctx, opPreferences, map[string]any{"id": sourceID}, &out); err != nil {
		return nil, err
	}
	res := make([]source.Preference, 0, len(out.Source.Preferences))
	for i, p := range out.Source.Preferences {
		pref := source.Preference{Position: i, Key: str(p["key"]), Title: str(p["title"]), Summary: str(p["summary"])}
		if v, ok := p["visible"].(bool); ok {
			pref.Visible = v
		}
		switch p["__typename"] {
		case "SwitchPreference":
			pref.Type, pref.Value, pref.DefaultValue = "switch", p["switchValue"], p["switchDefault"]
		case "CheckBoxPreference":
			pref.Type, pref.Value, pref.DefaultValue = "checkbox", p["checkValue"], p["checkDefault"]
		case "EditTextPreference":
			pref.Type, pref.Value, pref.DefaultValue = "edittext", p["textValue"], p["textDefault"]
		case "ListPreference":
			pref.Type, pref.Value, pref.DefaultValue = "list", p["listValue"], p["listDefault"]
			pref.Entries, pref.EntryValues = strs(p["entries"]), strs(p["entryValues"])
		case "MultiSelectListPreference":
			pref.Type, pref.Value, pref.DefaultValue = "multiselect", p["multiValue"], p["multiDefault"]
			pref.Entries, pref.EntryValues = strs(p["entries"]), strs(p["entryValues"])
		}
		res = append(res, pref)
	}
	return res, nil
}

func (m *Module) SetSourcePreference(ctx context.Context, sourceID string, position int, typ string, value any) error {
	change := map[string]any{"position": position}
	switch typ {
	case "switch":
		change["switchState"] = value
	case "checkbox":
		change["checkBoxState"] = value
	case "edittext":
		change["editTextState"] = value
	case "list":
		change["listState"] = value
	case "multiselect":
		change["multiSelectState"] = value
	default:
		return fmt.Errorf("unknown preference type %q", typ)
	}
	return m.c.do(ctx, opUpdatePreference, map[string]any{"source": sourceID, "change": change}, nil)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strs(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, str(x))
	}
	return out
}

// compile-time interface checks
var (
	_ source.Module           = (*Module)(nil)
	_ source.Latest           = (*Module)(nil)
	_ source.ExtensionManager = (*Module)(nil)
	_ source.Preferences      = (*Module)(nil)
	_ source.Thumbnails       = (*Module)(nil)
	_ source.Maintainer       = (*Module)(nil)
	_ source.Assets           = (*Module)(nil)
)

// HealthCheck pings the server and warns when its version differs from the pinned one.
func (m *Module) HealthCheck(ctx context.Context) (string, error) {
	a, err := m.About(ctx)
	if err != nil {
		return "", err
	}
	if a.Version != PinnedVersion {
		return fmt.Sprintf("Suwayomi %s differs from the tested version %s", a.Version, PinnedVersion), nil
	}
	return "", nil
}

// AutoUpdateExtensions reports whether extension updates are installed automatically.
func (m *Module) AutoUpdateExtensions() bool { return m.s.AutoUpdateExtensions }
