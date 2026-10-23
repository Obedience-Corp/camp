package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Obedience-Corp/obey-shared/updatecheck"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/version"
)

// versionCheckInfo is the camp version --check --json contract. Versions
// compared are suite versions: current_version is the festival release this
// binary shipped in, and camp_version is camp's own component version.
type versionCheckInfo struct {
	SchemaVersion   string `json:"schema_version"`
	Product         string `json:"product"`
	CampVersion     string `json:"camp_version"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	LatestTag       string `json:"latest_tag,omitempty"`
	Source          string `json:"source"`
	UpdateAvailable bool   `json:"update_available"`
	UpgradeKind     string `json:"upgrade_kind,omitempty"`
	UpgradeCommand  string `json:"upgrade_command,omitempty"`
	CheckedAt       string `json:"checked_at,omitempty"`
	FromCache       bool   `json:"from_cache"`
	RefreshError    string `json:"refresh_error,omitempty"`
	SkipReason      string `json:"skip_reason,omitempty"`
}

const (
	suiteSkipNotRelease = "not a festival suite release build"
	suiteSkipNoHome     = "cannot determine home directory"
)

func runVersionCheck(cmd *cobra.Command, jsonOut bool) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := suiteVersionCheck(ctx, version.Version, version.Bundle)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if jsonOut {
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return camperrors.Wrap(err, "marshal version check")
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	return writeVersionCheck(out, info)
}

// suiteVersionCheck compares bundle against the latest festival suite
// release. A build that did not ship in a suite release has nothing to
// compare, so it reads no cache and makes no request.
func suiteVersionCheck(ctx context.Context, campVersion, bundle string) (versionCheckInfo, error) {
	info := versionCheckInfo{
		SchemaVersion: version.CheckSchemaVersion,
		Product:       "camp",
		CampVersion:   campVersion,
		Source:        updatecheck.Source,
	}
	if bundle == "" {
		info.SkipReason = suiteSkipNotRelease
		return info, nil
	}
	info.CurrentVersion = updatecheck.NormalizeVersion(bundle)
	opts, ok := suiteCheckOptions()
	if !ok {
		info.SkipReason = suiteSkipNoHome
		return info, nil
	}
	res, err := suiteCheck(ctx, bundle, opts)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return info, camperrors.Wrap(err, "check festival suite version")
	}
	info.CurrentVersion = res.CurrentVersion
	info.LatestVersion = res.LatestVersion
	info.LatestTag = res.LatestTag
	if res.Source != "" {
		info.Source = res.Source
	}
	info.UpdateAvailable = res.UpdateAvailable
	if res.UpdateAvailable {
		info.UpgradeKind = res.UpgradeKind
		info.UpgradeCommand = res.UpgradeCommand
	}
	if !res.CheckedAt.IsZero() {
		info.CheckedAt = res.CheckedAt.UTC().Format(time.RFC3339)
	}
	info.FromCache = res.FromCache
	if res.RefreshErr != nil {
		info.RefreshError = res.RefreshErr.Error()
	}
	return info, nil
}

func writeVersionCheck(w io.Writer, info versionCheckInfo) error {
	lines := []string{}
	if info.CurrentVersion == "" {
		lines = append(lines, "camp "+info.CampVersion)
	} else {
		lines = append(lines, fmt.Sprintf("camp %s (festival suite %s)", info.CampVersion, info.CurrentVersion))
	}
	switch {
	case info.SkipReason != "":
		lines = append(lines, "festival suite update check skipped: "+info.SkipReason)
	case info.LatestVersion == "":
		lines = append(lines, "festival suite latest: unknown (could not reach GitHub)")
	default:
		latest := "festival suite latest: " + info.LatestVersion
		if info.RefreshError != "" {
			latest += " (cached " + info.CheckedAt + "; could not reach GitHub to refresh)"
		}
		lines = append(lines, latest)
		switch {
		case info.UpdateAvailable:
			lines = append(lines, "update available")
			if info.UpgradeCommand != "" {
				lines = append(lines, suiteActionLabel(info.UpgradeKind)+": "+info.UpgradeCommand)
			}
		case updatecheck.UpdateAvailable(info.LatestVersion, info.CurrentVersion):
			lines = append(lines, "newer than the latest festival suite release")
		default:
			lines = append(lines, "up to date")
		}
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func suiteActionLabel(kind string) string {
	if kind == "" {
		return "run"
	}
	return kind
}
