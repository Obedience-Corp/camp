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

	// Bundle is the festival suite version this binary shipped in (set via
	// ldflags by the festival release build). Empty for standalone builds.
	Bundle = ""
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	Version, Commit, BuildDate = applyBuildInfo(Version, Commit, BuildDate, info)
}

// applyBuildInfo fills unset ldflag fields from trusted module metadata.
// It copies info.Main.Version onto Version when Version is still the "dev"
// default and the module version is a real release, not "(devel)".
//
// vcs.revision and vcs.time are ignored. debug.BuildInfo has no VCS URL or
// repo identity, so those settings cannot be shown to belong to this module.
// When camp is built from a nested git worktree whose .git is a gitdir file,
// the Go toolchain may stamp an outer repository's commit instead.
func applyBuildInfo(version, commit, date string, info *debug.BuildInfo) (string, string, string) {
	if info == nil {
		return version, commit, date
	}
	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return version, commit, date
}

// Info contains the canonical snake_case JSON contract for camp version output.
type Info struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
	Bundle        string `json:"bundle,omitempty"`
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
		Bundle:        Bundle,
		Commit:        Commit,
		BuildDate:     BuildDate,
		GoVersion:     runtime.Version(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		Profile:       Profile,
	}
}
