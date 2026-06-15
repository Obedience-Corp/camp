// Package version provides build-time version information for camp.
// Variables are populated via ldflags during build.
package version

import (
	"runtime"
	"runtime/debug"
)

const SchemaVersion = "version/v1alpha1"

var (
	// Version is the semantic version (set via ldflags)
	Version = "dev"

	// Commit is the git commit hash (set via ldflags)
	Commit = "unknown"

	// BuildDate is the build timestamp (set via ldflags)
	BuildDate = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if Commit == "unknown" && len(setting.Value) >= 7 {
				Commit = setting.Value[:7]
			}
		case "vcs.time":
			if BuildDate == "unknown" && setting.Value != "" {
				BuildDate = setting.Value
			}
		}
	}
}

// Info contains the canonical snake_case JSON contract for camp version output.
type Info struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuildDate     string `json:"build_date"`
	GoVersion     string `json:"go_version"`
	Platform      string `json:"platform"`
	Profile       string `json:"profile"`
}

// Get returns the full version information.
func Get() Info {
	return Info{
		SchemaVersion: SchemaVersion,
		Version:       Version,
		Commit:        Commit,
		BuildDate:     BuildDate,
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		Profile:       Profile,
	}
}
