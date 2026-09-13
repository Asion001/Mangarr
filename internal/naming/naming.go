// Package naming renders file and folder names from templates.
//
// Tokens: {Series Title} {Series CleanTitle} {Series Year} {Chapter} {Chapter:0000}
// {Volume} {Volume:00} {Chapter Title} {Scanlator} {Source} {Language}
//
// Text inside [ ] is an optional group, rendered only when every token in it
// is non-empty: "{Series Title}[ Vol.{Volume:00}] Ch.{Chapter:0000}".
package naming

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Asion001/mangarr/internal/chapternum"
)

type Values struct {
	SeriesTitle  string
	SeriesYear   int
	Chapter      float64
	HasChapter   bool
	Volume       string
	ChapterTitle string
	Scanlator    string
	Source       string
	Language     string
}

var tokenRe = regexp.MustCompile(`\{([A-Za-z ]+)(?::([0-9]+))?\}`)

// Render renders tpl and sanitizes the result as a single path element.
func Render(tpl string, v Values) string {
	var out strings.Builder
	for i := 0; i < len(tpl); {
		switch tpl[i] {
		case '[':
			end := strings.IndexByte(tpl[i:], ']')
			if end < 0 {
				out.WriteString(tpl[i:])
				i = len(tpl)
				continue
			}
			group := tpl[i+1 : i+end]
			rendered, allSet := renderTokens(group, v)
			if allSet {
				out.WriteString(rendered)
			}
			i += end + 1
		default:
			next := strings.IndexByte(tpl[i:], '[')
			if next < 0 {
				next = len(tpl) - i
			}
			rendered, _ := renderTokens(tpl[i:i+next], v)
			out.WriteString(rendered)
			i += next
		}
	}
	return Sanitize(out.String())
}

func renderTokens(s string, v Values) (string, bool) {
	allSet := true
	res := tokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := tokenRe.FindStringSubmatch(tok)
		name := strings.ToLower(strings.TrimSpace(m[1]))
		width := 0
		if m[2] != "" {
			width = len(m[2])
		}
		val := token(name, width, v)
		if val == "" {
			allSet = false
		}
		return val
	})
	return res, allSet
}

func token(name string, width int, v Values) string {
	switch name {
	case "series title":
		return v.SeriesTitle
	case "series cleantitle":
		return CleanTitle(v.SeriesTitle)
	case "series year":
		if v.SeriesYear > 0 {
			return strconv.Itoa(v.SeriesYear)
		}
		return ""
	case "chapter":
		if !v.HasChapter {
			return ""
		}
		return chapternum.Format(v.Chapter, width)
	case "volume":
		if v.Volume == "" {
			return ""
		}
		if n, err := strconv.Atoi(v.Volume); err == nil && width > 0 {
			return fmt.Sprintf("%0*d", width, n)
		}
		return v.Volume
	case "chapter title":
		return v.ChapterTitle
	case "scanlator":
		return v.Scanlator
	case "source":
		return v.Source
	case "language":
		return v.Language
	default:
		return ""
	}
}

var (
	illegal    = strings.NewReplacer(": ", " - ", ":", "-", "/", "-", "\\", "-", "*", "", "?", "", "\"", "'", "<", "", ">", "", "|", "-")
	spaces     = regexp.MustCompile(`\s+`)
	cleanChars = regexp.MustCompile(`[^\p{L}\p{N} ]+`)
)

// Sanitize makes s safe as a single file/folder name on Linux, macOS and Windows.
func Sanitize(s string) string {
	s = illegal.Replace(s)
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = spaces.ReplaceAllString(s, " ")
	s = strings.Trim(s, " .")
	// keep names well below the 255 byte limit (extension + .partial are added)
	for len(s) > 200 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	s = strings.TrimRight(s, " .")
	if s == "" {
		s = "_"
	}
	return s
}

// CleanTitle strips punctuation: "Re:Zero - Starting Life" -> "ReZero Starting Life".
func CleanTitle(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(cleanChars.ReplaceAllString(s, ""), " "))
}

// SortTitle lowercases and drops leading articles for sorting.
func SortTitle(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(l, a) {
			return strings.TrimPrefix(l, a)
		}
	}
	return l
}
