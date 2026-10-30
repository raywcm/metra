package buildinfo

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version is also used by source builds without linker flags.
func Version() string { return strings.TrimSpace(version) }
