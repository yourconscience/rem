// Package rem is the library side of the rem memory tools. The cmd/rem binary
// embeds the hook and library files below so `rem install` can materialize
// them without the source checkout.
package rem

import "embed"

// Assets carries the hook and library files `rem install` writes into
// REM_HOME. go:embed can only reach files at or below this directory, so this
// file stays at the repository root. Files and directories beginning with '_'
// (such as lib/__pycache__) are excluded automatically.
//
//go:embed hooks lib
var Assets embed.FS
