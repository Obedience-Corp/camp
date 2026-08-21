package tasks

import "testing"

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
