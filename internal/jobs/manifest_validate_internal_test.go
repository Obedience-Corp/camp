package jobs

import (
	"strings"
	"testing"
)

// Queue files are hand-editable, so the manifest job's path components are
// re-validated at the claim boundary. An edited root or machine must never
// become a promise to walk or write outside the campaign's trees.
func TestManifestJobValidationRefusesEscapes(t *testing.T) {
	base := Job{
		Kind:            KindManifest,
		Class:           ClassManifest,
		Repo:            ".",
		ManifestRoot:    "media",
		DescribesCommit: "cafebabecafebabecafebabecafebabecafebabe",
		Machine:         "machine-a",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("the valid baseline must validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Job)
		want   string
	}{
		{"escaping root", func(j *Job) { j.ManifestRoot = "../../outside" }, "manifest_root"},
		{"absolute root", func(j *Job) { j.ManifestRoot = "/etc" }, "manifest_root"},
		{"machine with separator", func(j *Job) { j.Machine = "evil/../../tmp" }, "machine"},
		{"machine dot-prefixed", func(j *Job) { j.Machine = ".." }, "machine"},
		{"empty machine", func(j *Job) { j.Machine = "" }, "machine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := base
			tc.mutate(&j)
			err := j.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want refusal naming %s", err, tc.want)
			}
		})
	}
}
