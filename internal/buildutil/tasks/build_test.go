package tasks

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		describe string
		want     string
	}{
		{name: "no VERSION and no git description falls back to dev", env: "", describe: "", want: "dev"},
		{name: "blank git description falls back to dev", env: "", describe: "  \n", want: "dev"},
		{name: "blank VERSION falls through to the git description", env: "   ", describe: "v0.5.0", want: "v0.5.0"},
		{name: "exact tag is stamped as-is", env: "", describe: "v0.5.0", want: "v0.5.0"},
		{name: "untagged head keeps the distance and hash", env: "", describe: "v0.5.0-11-g30a8c1a9", want: "v0.5.0-11-g30a8c1a9"},
		{name: "dirty tree keeps the dirty suffix", env: "", describe: "v0.5.0-11-g30a8c1a9-dirty", want: "v0.5.0-11-g30a8c1a9-dirty"},
		{name: "git description is trimmed", env: "", describe: "v0.5.0\n", want: "v0.5.0"},
		{name: "VERSION overrides the git description", env: "v9.9.9", describe: "v0.5.0", want: "v9.9.9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFrom(tt.env, tt.describe); got != tt.want {
				t.Fatalf("versionFrom(%q, %q) = %q; want %q", tt.env, tt.describe, got, tt.want)
			}
		})
	}
}

// A cancelled context stands in for the wedged-git case the timeout exists to
// bound: the probe cannot answer, and the caller must fall back rather than
// stamp a partial version.
func TestGitProbesOnDeadContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := gitDescribe(ctx); got != "" {
		t.Errorf("gitDescribe(cancelled) = %q; want empty", got)
	}
	if got := gitCommit(ctx); got != "unknown" {
		t.Errorf("gitCommit(cancelled) = %q; want %q", got, "unknown")
	}
	if got := versionFrom(os.Getenv("NO_SUCH_VERSION_VAR"), gitDescribe(ctx)); got != "dev" {
		t.Errorf("versionFrom with a dead probe = %q; want %q", got, "dev")
	}
}

func TestBuildLDFlagsOnDeadContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Setenv("VERSION", "")

	got := buildLDFlags(ctx)
	for _, want := range []string{
		"-X " + versionPkg + ".Version=dev",
		"-X " + versionPkg + ".Commit=unknown",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildLDFlags(cancelled) = %q; want it to contain %q", got, want)
		}
	}
}
