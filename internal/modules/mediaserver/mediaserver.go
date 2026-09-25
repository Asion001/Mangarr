// Package mediaserver links metadata adaptations to media in configured servers.
package mediaserver

import (
	"context"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
)

type Module interface {
	modules.Instance
	// Find returns a web URL, or an empty string when no unambiguous match exists.
	Find(context.Context, model.Adaptation) (string, error)
}

type Item struct {
	ID            string
	Title         string
	OriginalTitle string
	Year          int
	Type          string // movie or series
	ProviderIDs   map[string]string
	URL           string
}

func NormalizeTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, norm.NFKC.String(s))
}

func accepts(format, kind string) bool {
	switch format {
	case "movie":
		return kind == "movie"
	case "tv", "tv_short":
		return kind == "series"
	case "ova", "ona", "special":
		return kind == "movie" || kind == "series"
	}
	return false
}

// Match prefers provider IDs across the whole result set. Conflicting IDs
// rule out a title fallback; ambiguous title/year matches produce no link.
func Match(a model.Adaptation, items []Item) string {
	var exact, fallback []Item
	title := NormalizeTitle(a.Title)
	for _, item := range items {
		if item.ID == "" || !accepts(a.Format, item.Type) {
			continue
		}
		matched, conflict := false, false
		for k, id := range item.ProviderIDs {
			key := strings.ToLower(k)
			if key == "myanimelist" {
				key = "mal"
			}
			if key != "anilist" && key != "mal" {
				continue
			}
			if want := a.ExternalIDs[key]; want != "" && id != "" {
				if want == id {
					matched = true
				} else {
					conflict = true
				}
			}
		}
		if conflict {
			continue
		}
		if matched {
			exact = append(exact, item)
			continue
		}
		if a.Year > 0 && item.Year == a.Year && title != "" &&
			(title == NormalizeTitle(item.Title) || title == NormalizeTitle(item.OriginalTitle)) {
			fallback = append(fallback, item)
		}
	}
	if len(exact) > 0 {
		return uniqueURL(exact)
	}
	return uniqueURL(fallback)
}

func uniqueURL(items []Item) string {
	var id, link string
	for _, item := range items {
		if id != "" && id != item.ID {
			return ""
		}
		id, link = item.ID, item.URL
	}
	return link
}
