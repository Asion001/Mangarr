// Package titlematch scores how likely two titles name the same series.
// Titles from sources differ from metadata titles in case, punctuation,
// diacritics, word order and small suffixes ("(Official)", "[Colored]"), so
// the score combines a token-set ratio with a normalized edit distance.
package titlematch

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// bracketed notes that don't change which series it is
	noise = regexp.MustCompile(`(?i)[\[(](official|colou?red|full colou?r|webtoon|digital|manhwa|manga|manhua|novel|raw|eng?|english|scan|pa?rt\s*\d+)[\])]`)
	// words that rarely distinguish series
	stop = map[string]bool{"the": true, "a": true, "an": true, "of": true, "no": true, "wa": true, "ga": true, "to": true, "and": true, "x": true}
)

// Normalize lowercases, removes diacritics and punctuation and collapses spaces.
func Normalize(s string) string {
	s = noise.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	space := true
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r): // combining marks (é -> e)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func tokens(s string) []string {
	var out []string
	for _, t := range strings.Fields(s) {
		if !stop[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		out = strings.Fields(s)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// levRatio is 1 - levenshtein(a,b)/max(len).
func levRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return 1 - float64(prev[len(rb)])/float64(max(len(ra), len(rb)))
}

// tokenSetRatio compares the shared tokens with each side's full token set
// (like fuzzywuzzy's token_set_ratio), so word order and extra words weigh less.
func tokenSetRatio(a, b string) float64 {
	ta, tb := tokens(a), tokens(b)
	var inter, onlyA, onlyB []string
	for _, t := range ta {
		if slices.Contains(tb, t) {
			inter = append(inter, t)
		} else {
			onlyA = append(onlyA, t)
		}
	}
	for _, t := range tb {
		if !slices.Contains(ta, t) {
			onlyB = append(onlyB, t)
		}
	}
	if len(inter) == 0 {
		return levRatio(strings.Join(ta, " "), strings.Join(tb, " "))
	}
	base := strings.Join(inter, " ")
	withA := strings.TrimSpace(base + " " + strings.Join(onlyA, " "))
	withB := strings.TrimSpace(base + " " + strings.Join(onlyB, " "))
	// penalize one side having many extra words (sequels, spin-offs)
	extra := float64(len(onlyA)+len(onlyB)) / float64(len(inter)+len(onlyA)+len(onlyB))
	r := max(levRatio(base, withA), levRatio(base, withB), levRatio(withA, withB))
	return r * (1 - 0.5*extra)
}

// Score returns the similarity of two titles in [0,1].
func Score(a, b string) float64 {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	if strings.ReplaceAll(na, " ", "") == strings.ReplaceAll(nb, " ", "") {
		return 0.99
	}
	return max(levRatio(na, nb), tokenSetRatio(na, nb))
}

// Best returns the highest score of candidate against any of titles.
func Best(titles []string, candidate string) float64 {
	best := 0.0
	for _, t := range titles {
		if s := Score(t, candidate); s > best {
			best = s
		}
	}
	return best
}
