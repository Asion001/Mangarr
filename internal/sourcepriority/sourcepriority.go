// Package sourcepriority resolves library, language and global catalog order.
// It does not enable sources, create links or queue downloads.
package sourcepriority

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/uptrace/bun"
)

func Language(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "ua" {
		return "uk"
	}
	return v
}
func Key(moduleID int64, sourceID string) string {
	return strconv.FormatInt(moduleID, 10) + ":" + sourceID
}
func LibraryScope(id int64) string     { return "library:" + strconv.FormatInt(id, 10) }
func LanguageScope(lang string) string { return "language:" + Language(lang) }

type Entry struct {
	Key      string
	Priority int
}

// Order is deterministic; unspecified entries keep their global priority.
// A source in both lists is placed only at its library position.
func Order(entries []Entry, library, language []string) map[string]int {
	explicit := map[string]int{}
	for _, list := range [][]string{library, language} {
		for _, key := range list {
			if _, ok := explicit[key]; !ok {
				explicit[key] = len(explicit)
			}
		}
	}
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, aok := explicit[sorted[i].Key]
		b, bok := explicit[sorted[j].Key]
		if aok != bok {
			return aok
		}
		if aok {
			return a < b
		}
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Key < sorted[j].Key
	})
	ranks := map[string]int{}
	for _, e := range sorted {
		if _, ok := ranks[e.Key]; !ok {
			ranks[e.Key] = len(ranks)
		}
	}
	return ranks
}

func Resolve(ctx context.Context, d bun.IDB, rootID int64, lang string, entries []Entry) (map[string]int, error) {
	var lists []model.SourcePriorityList
	if err := d.NewSelect().Model(&lists).Where("scope = ? OR scope = ?", LibraryScope(rootID), LanguageScope(lang)).Scan(ctx); err != nil {
		return nil, err
	}
	var library, language []string
	for _, l := range lists {
		if l.Scope == LibraryScope(rootID) {
			library = l.Sources
		} else {
			language = l.Sources
		}
	}
	return Order(entries, library, language), nil
}

// Ranks uses the same effective order for every release, including a file's
// current release, so inherited ranks cannot be compared with stored ranks.
func Ranks(ctx context.Context, d bun.IDB, ser model.Series, links []model.SeriesSource) (map[int64]int, error) {
	out := map[int64]int{}
	if ser.SourcePriorityMode != "inherit" {
		for _, l := range links {
			out[l.ID] = l.Priority
		}
		return out, nil
	}
	var prefs []model.CatalogPref
	if err := d.NewSelect().Model(&prefs).Scan(ctx); err != nil {
		return nil, err
	}
	global := map[string]int{}
	for _, p := range prefs {
		global[Key(p.ModuleID, p.SourceID)] = p.Priority
	}
	entries := make([]Entry, 0, len(links))
	for _, l := range links {
		key := Key(l.ModuleID, l.SourceID)
		priority, ok := global[key]
		if !ok {
			priority = 100
		}
		entries = append(entries, Entry{key, priority})
	}
	ranks, err := Resolve(ctx, d, ser.RootFolderID, ser.Language, entries)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		out[l.ID] = ranks[Key(l.ModuleID, l.SourceID)]
	}
	return out, nil
}

func Apply(ctx context.Context, d bun.IDB, ser model.Series, links []model.SeriesSource) error {
	ranks, err := Ranks(ctx, d, ser, links)
	if err != nil {
		return err
	}
	for i := range links {
		links[i].Priority = ranks[links[i].ID]
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Priority != links[j].Priority {
			return links[i].Priority < links[j].Priority
		}
		return links[i].ID < links[j].ID
	})
	return nil
}
