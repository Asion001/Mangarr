// Package redact removes secrets from text that leaves the server (log
// downloads, diagnostics bundles): known secret values, and anything that
// looks like a credential (api keys, tokens, passwords, basic auth in URLs).
package redact

import (
	"bufio"
	"io"
	"regexp"
	"sort"
	"strings"
)

// Mask replaces secrets.
const Mask = "***"

var patterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// user:password@ in URLs
	{regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`), "${1}" + Mask + ":" + Mask + "@"},
	// Authorization: Bearer xyz / Basic xyz
	{regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?)(bearer\s+|basic\s+)?[^\s"',&]+`), "${1}${2}" + Mask},
	// key=value, key: value, "key":"value" for credential-like keys
	{regexp.MustCompile(`(?i)((?:x-)?api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|session[_-]?secret)(["']?\s*[:=]\s*["']?)([^\s"',&}]+)`), "${1}${2}" + Mask},
}

// Redactor masks a fixed set of secret values plus credential patterns.
type Redactor struct {
	secrets []string
}

// New returns a redactor for the given secret values; values shorter than
// 6 characters are ignored (they would mask ordinary words).
func New(secrets []string) *Redactor {
	seen := map[string]bool{}
	var list []string
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) >= 6 && !seen[s] {
			seen[s] = true
			list = append(list, s)
		}
	}
	// longest first, so a secret containing another is masked whole
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	return &Redactor{secrets: list}
}

// String redacts one string.
func (r *Redactor) String(s string) string {
	for _, sec := range r.secrets {
		if strings.Contains(s, sec) {
			s = strings.ReplaceAll(s, sec, Mask)
		}
	}
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// Copy redacts r line by line into w.
func (r *Redactor) Copy(w io.Writer, src io.Reader) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	bw := bufio.NewWriter(w)
	for sc.Scan() {
		if _, err := bw.WriteString(r.String(sc.Text()) + "\n"); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return bw.Flush()
}
