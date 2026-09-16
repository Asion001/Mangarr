// Package sites holds mangarr's own source implementations: one file per
// site, each registering itself with the toolkit. Nothing here may import
// anything from mangarr except internal/sources/sourcekit — that rule is
// what lets these move to a repository of their own, and a test enforces it.
package sites

import (
	"errors"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// errorsAs is errors.As, kept here so sites don't each import errors.
func errorsAs(err error, target any) bool { return errors.As(err, target) }

// Count is how many sites are built in. The module refers to it so the
// sites' init functions run.
func Count() int { return len(sourcekit.Registered()) }
