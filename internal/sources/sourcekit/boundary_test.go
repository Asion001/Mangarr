package sourcekit_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSourcesStayPortable: the toolkit imports nothing else from mangarr, and
// the sites import only the toolkit. That is what makes this directory a
// `git mv` and a go.mod away from being its own repository — so if this test
// fails, move the helper you reached for into sourcekit instead.
func TestSourcesStayPortable(t *testing.T) {
	const self = "github.com/Asion001/mangarr/internal/sources/sourcekit"
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		inKit := strings.Contains(filepath.ToSlash(path), "/sources/sourcekit/")
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(p, "github.com/Asion001/mangarr/") {
				continue // the standard library and third parties are fine
			}
			rel, _ := filepath.Rel(root, path)
			switch {
			case inKit:
				t.Errorf("%s imports %s: the toolkit must not depend on the rest of mangarr", rel, p)
			case p != self:
				t.Errorf("%s imports %s: sites may only use %s", rel, p, self)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
