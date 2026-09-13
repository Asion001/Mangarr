// Package chapternum normalizes chapter numbers. The name parser is a Go
// port of Mihon's ChapterRecognition (Apache-2.0, github.com/mihonapp/mihon).
package chapternum

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

const numberPattern = `([0-9]+)(\.[0-9]+)?(\.?[a-z]+)?`

var (
	// "Mokushiroku Alice Vol.1 Ch. 4: Misrepresentation" -> 4
	basic = regexp.MustCompile(`ch\. *` + numberPattern)
	// "Bleach 567: Down With Snowwhite" -> 567
	number = regexp.MustCompile(numberPattern)
	// volume/season tags that should not be mistaken for the chapter number
	unwanted = regexp.MustCompile(`\b(?:v|ver|vol|version|volume|season|s)[^a-z]?[0-9]+`)
	// "One Piece 12 special" -> "One Piece 12special"
	unwantedWhiteSpace = regexp.MustCompile(`\s(extra|special|omake)`)
	volumeRe           = regexp.MustCompile(`(?i)\b(?:vol|volume)\.?\s*([0-9]+(?:\.[0-9]+)?)`)
)

// Parse returns the chapter number for a chapter name. reported is the
// number the source provided (-1 when unknown); it wins when valid.
// ok is false when no number could be determined.
func Parse(seriesTitle, chapterName string, reported float64) (n float64, ok bool) {
	if reported > -1 && !math.IsNaN(reported) {
		return Round(reported), true
	}
	clean := strings.ToLower(chapterName)
	if t := strings.TrimSpace(strings.ToLower(seriesTitle)); t != "" {
		clean = strings.ReplaceAll(clean, t, "")
	}
	clean = strings.TrimSpace(clean)
	clean = strings.NewReplacer(",", ".", "-", ".").Replace(clean)
	clean = unwantedWhiteSpace.ReplaceAllString(clean, "$1")

	matches := number.FindAllStringSubmatch(clean, -1)
	if len(matches) == 0 {
		return -1, false
	}
	if len(matches) > 1 {
		name := unwanted.ReplaceAllString(clean, "")
		if m := basic.FindStringSubmatch(name); m != nil {
			return Round(fromMatch(m[1], m[2], m[3])), true
		}
		if m := number.FindStringSubmatch(name); m != nil {
			return Round(fromMatch(m[1], m[2], m[3])), true
		}
	}
	m := matches[0]
	return Round(fromMatch(m[1], m[2], m[3])), true
}

func fromMatch(initial, decimal, alpha string) float64 {
	v, _ := strconv.ParseFloat(initial, 64)
	return v + decimalPart(decimal, alpha)
}

func decimalPart(decimal, alpha string) float64 {
	if decimal != "" {
		d, _ := strconv.ParseFloat(decimal, 64)
		return d
	}
	if alpha != "" {
		switch {
		case strings.Contains(alpha, "extra"):
			return 0.99
		case strings.Contains(alpha, "omake"):
			return 0.98
		case strings.Contains(alpha, "special"):
			return 0.97
		}
		a := strings.TrimLeft(alpha, ".")
		if len(a) == 1 {
			n := int(a[0]) - ('a' - 1)
			if n < 10 {
				return float64(n) / 10
			}
		}
	}
	return 0
}

// Round rounds to 3 decimals, removing float32 noise (10.100000381 -> 10.1).
func Round(n float64) float64 { return math.Round(n*1000) / 1000 }

// Key returns the canonical identity string of a chapter number ("12", "10.5").
func Key(n float64) string { return strconv.FormatFloat(Round(n), 'f', -1, 64) }

// Format renders n with the integer part zero-padded to width ("0012", "1044.5").
func Format(n float64, width int) string {
	k := Key(n)
	intPart, frac, hasFrac := strings.Cut(k, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	for len(intPart) < width {
		intPart = "0" + intPart
	}
	if neg {
		intPart = "-" + intPart
	}
	if hasFrac {
		return intPart + "." + frac
	}
	return intPart
}

// Volume extracts a volume number from a chapter name ("Vol.2 Ch.10" -> "2").
func Volume(chapterName string) string {
	if m := volumeRe.FindStringSubmatch(chapterName); m != nil {
		return strings.TrimLeft(m[1], "0")
	}
	return ""
}
