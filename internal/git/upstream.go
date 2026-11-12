package git

import (
	"context"
	"strconv"
	"strings"
)

// BehindUpstream reports how many commits the current branch of repoPath is
// behind its upstream, judged from refs already fetched: it never contacts the
// remote. ok is false when the answer is unknown (no upstream configured, a
// detached HEAD, or not a repository), which is different from "0 behind".
func BehindUpstream(ctx context.Context, repoPath string) (behind int, ok bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	out, err := gitCmd(ctx, repoPath, "rev-list", "--count", "HEAD..@{upstream}").Output()
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return n, true
}
