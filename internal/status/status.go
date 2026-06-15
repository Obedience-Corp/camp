// Package status collects repository status for campaign status commands.
package status

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
)

// Options configures status collection.
type Options struct {
	ShowRemoteURL bool
}

// RepoStatus holds the status of a single repository.
type RepoStatus struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Clean       bool   `json:"clean"`
	HasUpstream bool   `json:"has_upstream"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	Staged      int    `json:"staged"`
	Modified    int    `json:"modified"`
	Untracked   int    `json:"untracked"`
	Unmerged    int    `json:"unmerged"`
	StaleRefs   int    `json:"stale_refs"`
	Remote      string `json:"remote"`
	Error       string `json:"error,omitempty"`
}

// Collect gathers status for submodule paths relative to campRoot.
func Collect(ctx context.Context, campRoot string, paths []string, opts Options) []RepoStatus {
	statuses := make([]RepoStatus, 0, len(paths))

	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		name := git.SubmoduleDisplayName(p)
		status := GetRepoStatus(ctx, fullPath, name, false, opts)
		status.Path = p
		statuses = append(statuses, status)
	}

	return statuses
}

// GetRepoStatus collects status for one repository.
func GetRepoStatus(ctx context.Context, repoPath, name string, isCampaignRoot bool, opts Options) RepoStatus {
	rs := RepoStatus{
		Name: name,
		Path: repoPath,
	}

	branch, err := git.Output(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		rs.Error = "not a git repo"
		return rs
	}
	rs.Branch = branch

	if opts.ShowRemoteURL {
		if remote, err := git.Output(ctx, repoPath, "remote", "get-url", "origin"); err == nil {
			rs.Remote = ShortenRemoteURL(remote)
		}
	} else {
		if remote, err := git.Output(ctx, repoPath, "remote"); err == nil && remote != "" {
			names := strings.Split(remote, "\n")
			rs.Remote = strings.Join(names, ", ")
		}
	}

	statusArgs := []string{}
	if isCampaignRoot {
		statusArgs = append(statusArgs, "--ignore-submodules=all")
	}
	output, err := git.StatusPorcelain(ctx, repoPath, statusArgs...)
	if err != nil {
		rs.Error = "status failed"
		return rs
	}

	rs.Clean = len(output) == 0
	if !rs.Clean {
		for _, entry := range git.ParseStatusPorcelainZ(output) {
			if len(entry.Code) < 2 {
				continue
			}
			x, y := entry.Code[0], entry.Code[1]
			if x != ' ' && x != '?' {
				rs.Staged++
			}
			if y != ' ' && y != '?' {
				rs.Modified++
			}
			if x == '?' && y == '?' {
				rs.Untracked++
			}
		}
	}

	if isCampaignRoot {
		rs.StaleRefs = countStaleRefs(ctx, repoPath)
	}

	abOutput, err := git.Output(ctx, repoPath, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err == nil {
		rs.HasUpstream = true
		parts := strings.Fields(abOutput)
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &rs.Ahead)
			fmt.Sscanf(parts[1], "%d", &rs.Behind)
		}
	}

	rs.Unmerged = git.UnmergedBranchCount(ctx, repoPath)

	return rs
}

func countStaleRefs(ctx context.Context, repoPath string) int {
	output, err := git.Output(ctx, repoPath, "submodule", "status")
	if err != nil || output == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if len(line) > 0 && line[0] == '+' {
			count++
		}
	}
	return count
}

// ShortenRemoteURL converts common GitHub remote URLs to owner/repo form.
func ShortenRemoteURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if strings.HasPrefix(url, "https://github.com/") {
		return strings.TrimPrefix(url, "https://github.com/")
	}
	if strings.HasPrefix(url, "git@github.com:") {
		return strings.TrimPrefix(url, "git@github.com:")
	}
	return url
}
