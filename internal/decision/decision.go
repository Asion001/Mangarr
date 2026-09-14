// Package decision chooses which release (source + scanlator) of a chapter
// to download, Sonarr-style: a list of specifications rejects candidates,
// and the survivors are ranked by source priority, then scanlator preference,
// then earliest upload.
package decision

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

type Candidate struct {
	Release model.ChapterRelease
	Source  model.SeriesSource
}

type Input struct {
	Series  model.Series
	Profile model.Profile
	Chapter model.Chapter
	// CurrentFile is the imported file (nil when missing).
	CurrentFile *model.ChapterFile
	// CurrentRelease is the candidate the current file came from (if known).
	CurrentRelease *Candidate
	Queued         bool
	Blocklisted    func(seriesSourceID int64, chapterURL string) bool
	Now            time.Time
	// Search is true for explicit searches (ignores chapter monitoring).
	Search bool
}

type Rejection struct {
	ReleaseID int64  `json:"releaseId,omitempty"`
	Reason    string `json:"reason"`
	Temporary bool   `json:"temporary"`
}

type Decision struct {
	ChapterID  int64       `json:"chapterId"`
	Approved   *Candidate  `json:"-"`
	IsUpgrade  bool        `json:"isUpgrade"`
	Rejections []Rejection `json:"rejections"`
}

// Decide evaluates all candidates of one chapter.
func Decide(in Input, cands []Candidate) Decision {
	d := Decision{ChapterID: in.Chapter.ID}
	reject := func(reason string, temp bool) Decision {
		d.Rejections = append(d.Rejections, Rejection{Reason: reason, Temporary: temp})
		return d
	}
	// chapter-level specifications
	if !in.Search && !in.Series.Monitored {
		return reject("series is not monitored", false)
	}
	if !in.Search && !in.Chapter.Monitored {
		return reject("chapter is not monitored", false)
	}
	if in.Chapter.State == model.ChapterCleaned && !in.Search {
		return reject("chapter was cleaned after being read", false)
	}
	if in.Queued {
		return reject("chapter is already queued", true)
	}
	if in.CurrentFile != nil && !in.Profile.Config.AllowUpgrades {
		return reject("chapter is already downloaded and upgrades are disabled", false)
	}
	if len(cands) == 0 {
		return reject("no release available", true)
	}

	prefs := compileAll(in.Profile.Config.PreferredScanlators)
	blocked := compileAll(in.Profile.Config.BlockedScanlators)
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	var ok []Candidate
	for _, c := range cands {
		rid := c.Release.ID
		switch {
		case c.Release.Removed:
			d.Rejections = append(d.Rejections, Rejection{rid, "release was removed from the source", false})
		case !c.Source.Enabled:
			d.Rejections = append(d.Rejections, Rejection{rid, "source is disabled", false})
		case c.Source.BackoffUntil != nil && c.Source.BackoffUntil.After(now):
			d.Rejections = append(d.Rejections, Rejection{rid, "source is failing, retrying later", true})
		case in.Blocklisted != nil && in.Blocklisted(c.Source.ID, c.Release.ChapterURL):
			d.Rejections = append(d.Rejections, Rejection{rid, "release is blocklisted", false})
		case matchAny(blocked, c.Release.Scanlator):
			d.Rejections = append(d.Rejections, Rejection{rid, "scanlator " + c.Release.Scanlator + " is blocked by the profile", false})
		case blockedForSeries(in.Series.BlockedScanlators, c.Release.Scanlator):
			d.Rejections = append(d.Rejections, Rejection{rid, "scanlator " + c.Release.Scanlator + " is blocked for this series", false})
		default:
			ok = append(ok, c)
		}
	}
	if len(ok) == 0 {
		return d
	}
	sort.SliceStable(ok, func(i, j int) bool { return less(ok[i], ok[j], prefs) })
	best := ok[0]
	if in.CurrentFile != nil {
		if in.CurrentRelease != nil && !Better(best, *in.CurrentRelease, prefs) {
			d.Rejections = append(d.Rejections, Rejection{best.Release.ID, "existing file is from an equal or better release", false})
			return d
		}
		if in.CurrentRelease == nil && in.CurrentFile.ReleaseID != nil && *in.CurrentFile.ReleaseID == best.Release.ID {
			d.Rejections = append(d.Rejections, Rejection{best.Release.ID, "existing file is from this release", false})
			return d
		}
		d.IsUpgrade = true
	}
	d.Approved = &best
	return d
}

// Rank key: lower is better.
type rank struct {
	priority int
	scanIdx  int
}

func rankOf(c Candidate, prefs []*regexp.Regexp) rank {
	return rank{priority: c.Source.Priority, scanIdx: scanlatorIndex(prefs, c.Release.Scanlator)}
}

// Better reports whether a is strictly better than b (upgrade-worthy).
func Better(a, b Candidate, prefs []*regexp.Regexp) bool {
	ra, rb := rankOf(a, prefs), rankOf(b, prefs)
	if ra.priority != rb.priority {
		return ra.priority < rb.priority
	}
	return ra.scanIdx < rb.scanIdx
}

func less(a, b Candidate, prefs []*regexp.Regexp) bool {
	if Better(a, b, prefs) {
		return true
	}
	if Better(b, a, prefs) {
		return false
	}
	ta, tb := a.Release.UploadDate, b.Release.UploadDate
	switch {
	case ta != nil && tb != nil && !ta.Equal(*tb):
		return ta.Before(*tb)
	case ta != nil && tb == nil:
		return true
	case ta == nil && tb != nil:
		return false
	}
	return a.Release.ID < b.Release.ID
}

func scanlatorIndex(prefs []*regexp.Regexp, scanlator string) int {
	for i, re := range prefs {
		if scanlator != "" && re.MatchString(scanlator) {
			return i
		}
	}
	return len(prefs)
}

func matchAny(res []*regexp.Regexp, s string) bool {
	if s == "" {
		return false
	}
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// blockedForSeries matches literal scanlator names (case-insensitive; a
// release by several groups is blocked when any of them is).
func blockedForSeries(names []string, scanlator string) bool {
	if scanlator == "" || len(names) == 0 {
		return false
	}
	parts := strings.FieldsFunc(strings.ToLower(scanlator), func(r rune) bool { return r == '&' || r == ',' || r == '|' })
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == strings.ToLower(strings.TrimSpace(scanlator)) {
			return true
		}
		for _, p := range parts {
			if n == strings.TrimSpace(p) {
				return true
			}
		}
	}
	return false
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*regexp.Regexp{}
)

// CompilePattern compiles a case-insensitive scanlator pattern (cached).
func CompilePattern(p string) (*regexp.Regexp, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if re, ok := cache[p]; ok {
		return re, nil
	}
	re, err := regexp.Compile("(?i)" + p)
	if err != nil {
		return nil, err
	}
	cache[p] = re
	return re, nil
}

func compileAll(ps []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(ps))
	for _, p := range ps {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if re, err := CompilePattern(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// Preferences compiles a profile's preferred scanlator patterns.
func Preferences(p model.Profile) []*regexp.Regexp { return compileAll(p.Config.PreferredScanlators) }
