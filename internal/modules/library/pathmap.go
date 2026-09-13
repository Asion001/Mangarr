package library

import (
	"path"
	"sort"
	"strings"
)

// PathMap translates between mangarr paths and a library server's paths
// (like Sonarr's remote path mappings), e.g. "/data/manga" -> "/manga".
type PathMap struct {
	pairs [][2]string // local, remote (longest local first)
}

func NewPathMap(m map[string]string) PathMap {
	var pm PathMap
	for local, remote := range m {
		local, remote = clean(local), clean(remote)
		if local != "" && remote != "" {
			pm.pairs = append(pm.pairs, [2]string{local, remote})
		}
	}
	sort.Slice(pm.pairs, func(i, j int) bool { return len(pm.pairs[i][0]) > len(pm.pairs[j][0]) })
	return pm
}

func clean(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ""
	}
	c := path.Clean(p)
	return c
}

// ToRemote maps a local path to the server's view.
func (pm PathMap) ToRemote(local string) string { return pm.swap(local, 0, 1) }

// ToLocal maps a server path back to mangarr's view.
func (pm PathMap) ToLocal(remote string) string { return pm.swap(remote, 1, 0) }

func (pm PathMap) swap(p string, from, to int) string {
	p = clean(p)
	pairs := append([][2]string(nil), pm.pairs...)
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i][from]) > len(pairs[j][from]) })
	for _, pr := range pairs {
		if p == pr[from] {
			return pr[to]
		}
		if strings.HasPrefix(p, pr[from]+"/") {
			return pr[to] + p[len(pr[from]):]
		}
	}
	return p
}

// Under reports whether p equals root or is inside it.
func Under(p, root string) bool {
	p, root = clean(p), clean(root)
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
