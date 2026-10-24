package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Obedience-Corp/obey-shared/updatecheck"

	"github.com/Obedience-Corp/camp/internal/version"
)

func stubSuiteCheck(t *testing.T, res updatecheck.Result) *[]string {
	t.Helper()
	prevHome, prevCheck := suiteHome, suiteCheck
	t.Cleanup(func() { suiteHome, suiteCheck = prevHome, prevCheck })
	var locals []string
	suiteHome = func() (string, error) { return "/fixture/home", nil }
	suiteCheck = func(_ context.Context, local string, opts updatecheck.Options) (updatecheck.Result, error) {
		if opts.CachePath != "/fixture/home/.obey/update-check/suite.json" {
			t.Errorf("CachePath = %q", opts.CachePath)
		}
		locals = append(locals, local)
		return res, nil
	}
	return &locals
}

func TestVersionCheck_EmptyBundleSkipsWithoutChecking(t *testing.T) {
	locals := stubSuiteCheck(t, updatecheck.Result{LatestVersion: "0.3.24"})

	info, err := suiteVersionCheck(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*locals) != 0 {
		t.Fatalf("checker ran for a build outside a suite release: %v", *locals)
	}
	if info.SkipReason != suiteSkipNotRelease || info.UpdateAvailable || info.CurrentVersion != "" {
		t.Fatalf("info = %+v", info)
	}

	var out bytes.Buffer
	if err := writeVersionCheck(&out, info); err != nil {
		t.Fatal(err)
	}
	want := "camp dev\nfestival suite update check skipped: not a festival suite release build\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestVersionCheck_JSONContract(t *testing.T) {
	checked := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	locals := stubSuiteCheck(t, updatecheck.Result{
		CurrentVersion:  "0.3.20",
		LatestVersion:   "0.3.24",
		LatestTag:       "v0.3.24",
		Source:          updatecheck.Source,
		UpdateAvailable: true,
		UpgradeKind:     "run",
		UpgradeCommand:  "festival update",
		CheckedAt:       checked,
	})

	info, err := suiteVersionCheck(context.Background(), "0.16.3", "v0.3.20")
	if err != nil {
		t.Fatal(err)
	}
	if len(*locals) != 1 || (*locals)[0] != "v0.3.20" {
		t.Fatalf("checker got %v, want the suite bundle", *locals)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"schema_version":    version.CheckSchemaVersion,
		"product":           "camp",
		"component_version": "0.16.3",
		"current_version":   "0.3.20",
		"latest_version":    "0.3.24",
		"latest_tag":        "v0.3.24",
		"source":            updatecheck.Source,
		"update_available":  true,
		"upgrade_kind":      "run",
		"upgrade_command":   "festival update",
		"checked_at":        "2026-10-23T12:00:00Z",
		"from_cache":        false,
	}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestVersionCheck_HumanOutput(t *testing.T) {
	cases := []struct {
		name string
		info versionCheckInfo
		want string
	}{
		{
			name: "update with a command",
			info: versionCheckInfo{ComponentVersion: "0.16.3", CurrentVersion: "0.3.20", LatestVersion: "0.3.24", UpdateAvailable: true, UpgradeKind: "run", UpgradeCommand: "festival update"},
			want: "camp 0.16.3 (festival suite 0.3.20)\nfestival suite latest: 0.3.24\nupdate available\nrun: festival update\n",
		},
		{
			name: "update with a page",
			info: versionCheckInfo{ComponentVersion: "0.16.3", CurrentVersion: "0.3.20", LatestVersion: "0.3.24", UpdateAvailable: true, UpgradeKind: "see", UpgradeCommand: "https://example.test/latest"},
			want: "camp 0.16.3 (festival suite 0.3.20)\nfestival suite latest: 0.3.24\nupdate available\nsee: https://example.test/latest\n",
		},
		{
			name: "current",
			info: versionCheckInfo{ComponentVersion: "0.16.3", CurrentVersion: "0.3.24", LatestVersion: "0.3.24"},
			want: "camp 0.16.3 (festival suite 0.3.24)\nfestival suite latest: 0.3.24\nup to date\n",
		},
		{
			name: "ahead of the release",
			info: versionCheckInfo{ComponentVersion: "0.16.3", CurrentVersion: "0.3.25", LatestVersion: "0.3.24"},
			want: "camp 0.16.3 (festival suite 0.3.25)\nfestival suite latest: 0.3.24\nnewer than the latest festival suite release\n",
		},
		{
			name: "release unknown",
			info: versionCheckInfo{ComponentVersion: "0.16.3", CurrentVersion: "0.3.20", Source: updatecheck.Source, RefreshError: "dial tcp: i/o timeout"},
			want: "camp 0.16.3 (festival suite 0.3.20)\nfestival suite latest: unknown (could not refresh from GitHub)\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeVersionCheck(&out, tc.info); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("output = %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestVersionCheck_FlagRoutesToCheck(t *testing.T) {
	locals := stubSuiteCheck(t, updatecheck.Result{CurrentVersion: "0.3.24", LatestVersion: "0.3.24"})
	prevBundle, prevVersion := version.Bundle, version.Version
	t.Cleanup(func() { version.Bundle, version.Version = prevBundle, prevVersion })
	version.Bundle, version.Version = "v0.3.24", "0.16.3"

	cmd, out := newVersionTestCommand(t)
	for _, flag := range []string{"check", "short"} {
		if err := cmd.Flags().Set(flag, "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err := runVersion(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if len(*locals) != 1 || (*locals)[0] != "v0.3.24" {
		t.Fatalf("checker got %v, want the suite bundle v0.3.24", *locals)
	}
	want := "camp 0.16.3 (festival suite 0.3.24)\nfestival suite latest: 0.3.24\nup to date\n"
	if out.String() != want {
		t.Fatalf("--check --short output = %q, want %q", out.String(), want)
	}
}

// A stale cache plus an unreachable GitHub must not read as a confident
// answer: the person asked explicitly, so the failed refresh is part of it.
func TestVersionCheck_ReportsAFailedRefresh(t *testing.T) {
	stubSuiteCheck(t, updatecheck.Result{
		CurrentVersion: "0.3.20",
		LatestVersion:  "0.3.20",
		LatestTag:      "v0.3.20",
		Source:         updatecheck.Source,
		CheckedAt:      time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		FromCache:      true,
		RefreshErr:     errors.New("dial tcp: i/o timeout"),
	})

	info, err := suiteVersionCheck(context.Background(), "0.16.3", "v0.3.20")
	if err != nil {
		t.Fatal(err)
	}
	if !info.FromCache || info.RefreshError == "" {
		t.Fatalf("failed refresh missing from the JSON contract: %+v", info)
	}
	var out bytes.Buffer
	if err := writeVersionCheck(&out, info); err != nil {
		t.Fatal(err)
	}
	want := "camp 0.16.3 (festival suite 0.3.20)\n" +
		"festival suite latest: 0.3.20 (cached 2026-10-20T09:00:00Z; refresh from GitHub failed)\n" +
		"up to date\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

// Ctrl-C during the explicit fetch is the user stopping camp, not a network
// failure to report as a result.
func TestVersionCheck_CancelledContextIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stubSuiteCheck(t, updatecheck.Result{CurrentVersion: "0.3.20", Source: updatecheck.Source})
	suiteCheck = func(context.Context, string, updatecheck.Options) (updatecheck.Result, error) {
		cancel()
		return updatecheck.Result{CurrentVersion: "0.3.20", RefreshErr: context.Canceled}, nil
	}

	if _, err := suiteVersionCheck(ctx, "0.16.3", "v0.3.20"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
