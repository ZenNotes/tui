package cli

import (
	"runtime/debug"
	"strings"
)

// defaultVersion is what the source says. Releases stamp Version through
// -ldflags (see .goreleaser.yaml) and that must win over anything below.
const defaultVersion = "0.1.0"

// Version is what `zn --version` and the help banner print.
var Version = defaultVersion

// `go install ...@version` and a plain checkout build carry no ldflags, so
// fall back to what the Go toolchain recorded in the binary: the module
// version for an install (a pseudo-version between tags), the commit for a
// checkout. `zn --version` then names the build a bug report is on.
func init() {
	if Version != defaultVersion {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return
	}
	if v := strings.TrimPrefix(info.Main.Version, "v"); v != "" && v != "(devel)" {
		Version = v
		return
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if revision != "" {
		Version += "+" + revision
		if modified {
			Version += ".dirty"
		}
	}
}
