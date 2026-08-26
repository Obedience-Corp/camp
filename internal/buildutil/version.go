package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const versionPkg = "github.com/Obedience-Corp/camp/internal/version"

// gitProbeTimeout bounds the version probes as a whole. A wedged git (a stale
// index.lock, an unreachable filesystem) must cost the build a few seconds and
// an unstamped version, never an indefinite hang.
const gitProbeTimeout = 10 * time.Second

func ldflags() string {
	return ldflagsFrom(context.Background())
}

func ldflagsFrom(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, gitProbeTimeout)
	defer cancel()

	version := versionFrom(os.Getenv("VERSION"), gitDescribe(ctx))
	commit := gitCommit(ctx)
	buildDate := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	return fmt.Sprintf("-X %s.Version=%s -X %s.Commit=%s -X %s.BuildDate=%s",
		versionPkg, version, versionPkg, commit, versionPkg, buildDate)
}

func versionFrom(env, describe string) string {
	if v := strings.TrimSpace(env); v != "" {
		return v
	}
	if d := strings.TrimSpace(describe); d != "" {
		return d
	}
	return "dev"
}

func gitDescribe(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitCommit(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	if commit := strings.TrimSpace(string(out)); commit != "" {
		return commit
	}
	return "unknown"
}
