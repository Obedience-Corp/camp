package version

import (
	"encoding/json"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %s; want %s", info.SchemaVersion, SchemaVersion)
	}

	// Check that basic fields are populated
	if info.Version == "" {
		t.Error("Version should not be empty")
	}

	if info.Commit == "" {
		t.Error("Commit should not be empty")
	}

	if info.BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}

	// Check that runtime info is correct
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %s; want %s", info.GoVersion, runtime.Version())
	}

	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expectedPlatform {
		t.Errorf("Platform = %s; want %s", info.Platform, expectedPlatform)
	}

	if info.Profile != Profile {
		t.Errorf("Profile = %s; want %s", info.Profile, Profile)
	}
}

func TestDefaultValues(t *testing.T) {
	// Test that default values are set
	if Version == "" {
		t.Error("Version should have a default value")
	}

	if Commit == "" {
		t.Error("Commit should have a default value")
	}

	if BuildDate == "" {
		t.Error("BuildDate should have a default value")
	}

	if Profile == "" {
		t.Error("Profile should have a default value")
	}
}

func TestApplyBuildInfo(t *testing.T) {
	foreign := "ec40648deadbeefcafebabe"
	infoWithVCS := func(mainVer string) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "github.com/Obedience-Corp/camp", Version: mainVer},
			Settings: []debug.BuildSetting{
				{Key: "vcs", Value: "git"},
				{Key: "vcs.revision", Value: foreign},
				{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}
	}

	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		info    *debug.BuildInfo
		wantV   string
		wantC   string
		wantD   string
	}{
		{
			name:    "foreign vcs.revision does not stamp commit",
			version: "dev",
			commit:  "unknown",
			date:    "unknown",
			info:    infoWithVCS("(devel)"),
			wantV:   "dev",
			wantC:   "unknown",
			wantD:   "unknown",
		},
		{
			name:    "module version fills Version",
			version: "dev",
			commit:  "unknown",
			date:    "unknown",
			info:    infoWithVCS("v1.2.3"),
			wantV:   "v1.2.3",
			wantC:   "unknown",
			wantD:   "unknown",
		},
		{
			name:    "ldflags commit and date win over vcs settings",
			version: "dev",
			commit:  "abc1234",
			date:    "2026-08-21T00:00:00Z",
			info:    infoWithVCS("(devel)"),
			wantV:   "dev",
			wantC:   "abc1234",
			wantD:   "2026-08-21T00:00:00Z",
		},
		{
			name:    "ldflags version is not overwritten by module version",
			version: "v9.9.9",
			commit:  "unknown",
			date:    "unknown",
			info:    infoWithVCS("v1.2.3"),
			wantV:   "v9.9.9",
			wantC:   "unknown",
			wantD:   "unknown",
		},
		{
			name:    "nil info is a no-op",
			version: "dev",
			commit:  "unknown",
			date:    "unknown",
			info:    nil,
			wantV:   "dev",
			wantC:   "unknown",
			wantD:   "unknown",
		},
		{
			name:    "empty main version keeps dev",
			version: "dev",
			commit:  "unknown",
			date:    "unknown",
			info:    &debug.BuildInfo{Main: debug.Module{Path: "github.com/Obedience-Corp/camp"}},
			wantV:   "dev",
			wantC:   "unknown",
			wantD:   "unknown",
		},
		{
			name:    "short vcs.revision is still ignored",
			version: "dev",
			commit:  "unknown",
			date:    "unknown",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc"},
					{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"},
				},
			},
			wantV: "dev",
			wantC: "unknown",
			wantD: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotC, gotD := applyBuildInfo(tt.version, tt.commit, tt.date, tt.info)
			if gotV != tt.wantV || gotC != tt.wantC || gotD != tt.wantD {
				t.Fatalf("applyBuildInfo() = (%q, %q, %q); want (%q, %q, %q)",
					gotV, gotC, gotD, tt.wantV, tt.wantC, tt.wantD)
			}
		})
	}
}

func TestBundleDefaultsEmpty(t *testing.T) {
	// Only the festival release build stamps Bundle. A camp built from source
	// has no suite around it, so the default must stay empty.
	if Bundle != "" {
		t.Errorf("Bundle = %q; want empty by default", Bundle)
	}
}

func TestGetBundle(t *testing.T) {
	original := Bundle
	t.Cleanup(func() { Bundle = original })

	tests := []struct {
		name     string
		bundle   string
		wantJSON bool
	}{
		{name: "unset bundle is omitted from JSON", bundle: "", wantJSON: false},
		{name: "festival suite version is carried", bundle: "v0.2.17", wantJSON: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Bundle = tt.bundle

			info := Get()
			if info.Bundle != tt.bundle {
				t.Fatalf("Get().Bundle = %q; want %q", info.Bundle, tt.bundle)
			}

			raw, err := json.Marshal(info)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var payload map[string]string
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, raw)
			}

			got, ok := payload["bundle"]
			if ok != tt.wantJSON {
				t.Fatalf("bundle key present = %v; want %v\nraw: %s", ok, tt.wantJSON, raw)
			}
			if tt.wantJSON && got != tt.bundle {
				t.Fatalf("bundle = %q; want %q", got, tt.bundle)
			}
		})
	}
}
