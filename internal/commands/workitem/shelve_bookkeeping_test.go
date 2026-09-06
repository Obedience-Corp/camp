//go:build container_fs

package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/defercommit"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// initGitCampaign turns a promoteCampaign root into a git repository with one
// commit, so a move can be auto-committed and its commit inspected.
func initGitCampaign(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

func headSubject(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func movedUnderCompleted(t *testing.T, root, slug string) {
	t.Helper()
	if dungeonWorkitemDir(t, root, "design", "completed", slug) == "" {
		t.Fatalf("moved workitem %q not found under the completed dungeon", slug)
	}
}

func auditHasPromote(t *testing.T, root string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"event":"promote"`)
}

// A link registry that cannot be opened used to stop a promotion right after
// the directory had moved: no audit line, no ledger event, no commit, and the
// move surfaced later inside whatever commit somebody made next. The move must
// be recorded and committed regardless, and the failure reported as a warning.
func TestPromoteCommitsAndRecordsWhenLinkReleaseFails(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "1")
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")
	initGitCampaign(t, root)
	writeFile(t, links.LinksPath(root), "links: [not yaml\n")

	out, errOut, err := execPromote(t, src, "--target", "completed")
	if err != nil {
		t.Fatalf("a bookkeeping failure must not fail the promotion: %v\n%s%s", err, out, errOut)
	}
	movedUnderCompleted(t, root, "feat")
	if dirExists(src) {
		t.Fatal("source directory is still present after the move")
	}
	if subject := headSubject(t, root); !strings.Contains(subject, "dungeon crawl completed") {
		t.Fatalf("the move was not committed: HEAD is %q", subject)
	}
	if !auditHasPromote(t, root) {
		t.Fatal("the promote event is missing from the audit file")
	}
	if !strings.Contains(errOut, "shelve bookkeeping failed") || !strings.Contains(errOut, "doctor --fix") {
		t.Fatalf("the failure was not reported as a warning:\n%s%s", out, errOut)
	}
}

// --json promised a real commit hash in its document. It never enters the
// deferred queue, even where a bookkeeping commit otherwise would.
func TestPromoteJSONCommitsSynchronously(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "")
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")
	initGitCampaign(t, root)
	writeFile(t, links.LinksPath(root), "links: [not yaml\n")

	out, _, err := execPromote(t, src, "--target", "completed", "--json")
	if err != nil {
		t.Fatalf("json promote: %v\n%s", err, out)
	}
	var res struct {
		Committed     bool     `json:"committed"`
		CommitMessage string   `json:"commit_message"`
		Hash          string   `json:"hash"`
		Warnings      []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if !res.Committed || res.Hash == "" || strings.Contains(res.CommitMessage, "queued") {
		t.Fatalf("--json must commit synchronously: %+v", res)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "shelve bookkeeping failed") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	if subject := headSubject(t, root); !strings.Contains(subject, "dungeon crawl completed") {
		t.Fatalf("HEAD is %q", subject)
	}
}

// The merged-branch backstop and camp workitem sweep share this path.
func TestPromoteMergedWorkitemCommitsWhenLinkReleaseFails(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "1")
	root := promoteCampaign(t)
	addWorkitem(t, root, "design", "feat", "Feat", "body")
	initGitCampaign(t, root)
	writeFile(t, links.LinksPath(root), "links: [not yaml\n")

	ctx := context.Background()
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	wi := wkitem.WorkItem{
		WorkflowType: wkitem.WorkflowTypeDesign,
		RelativePath: "workflow/design/feat",
		StableID:     "design-feat-fixed",
		Key:          "design:workflow/design/feat",
	}
	var out bytes.Buffer
	if err := PromoteMergedWorkitem(ctx, &out, cfg, root, wi, wkitem.EvidenceMergedBranch); err != nil {
		t.Fatalf("backstop promote must not fail on bookkeeping: %v\n%s", err, out.String())
	}
	movedUnderCompleted(t, root, "feat")
	if subject := headSubject(t, root); !strings.Contains(subject, "dungeon crawl completed") {
		t.Fatalf("the move was not committed: HEAD is %q", subject)
	}
	if !auditHasPromote(t, root) {
		t.Fatal("the promote event is missing from the audit file")
	}
	if !strings.Contains(out.String(), "releasing its links failed") {
		t.Fatalf("warning not printed:\n%s", out.String())
	}
}

// A caller that stops waiting after the directory moved still gets a
// recorded, committed move.
func TestPromoteFinishesAfterCallerCancellation(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "1")
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")
	initGitCampaign(t, root)

	cfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	wi := wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "workflow/design/feat", StableID: "design-feat-fixed", Key: "design:workflow/design/feat"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer close(done)
	// Cancel as soon as the source is gone: MoveToDungeon is the first
	// mutation, and everything after it must ignore the cancellation.
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			if !dirExists(src) {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	var out bytes.Buffer
	err = PromoteMergedWorkitem(ctx, &out, cfg, root, wi, wkitem.EvidenceMergedBranch)
	cancel()
	if err != nil {
		t.Fatalf("promotion after cancellation: %v\n%s", err, out.String())
	}
	if subject := headSubject(t, root); !strings.Contains(subject, "dungeon crawl completed") {
		t.Fatalf("the move was not committed: HEAD is %q", subject)
	}
	if !auditHasPromote(t, root) {
		t.Fatal("the promote event is missing from the audit file")
	}
}

func TestPromoteMergedWorkitem_IntentMovesToDone(t *testing.T) {
	t.Setenv(defercommit.EnvNoDefer, "1")
	root, relPath := intentTestCampaign(t)
	initGitCampaign(t, root)

	ctx := context.Background()
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	wi := wkitem.WorkItem{
		WorkflowType: wkitem.WorkflowTypeIntent,
		SourceID:     intentTestID,
		RelativePath: relPath,
		Key:          "intent:" + relPath,
		ItemKind:     wkitem.ItemKindFile,
	}
	var out bytes.Buffer
	if err := PromoteMergedWorkitem(ctx, &out, cfg, root, wi, wkitem.EvidenceMergedBranch); err != nil {
		t.Fatalf("intent promote: %v\n%s", err, out.String())
	}
	matches, err := filepath.Glob(filepath.Join(root, ".campaign", "intents", "*", "done", intentTestID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("intent not under intents/*/done/: glob matches %v", matches)
	}
	if !strings.Contains(filepath.ToSlash(matches[0]), ".campaign/intents/") || !strings.Contains(filepath.ToSlash(matches[0]), "/done/") {
		t.Fatalf("intent landed at unexpected path: %s", matches[0])
	}
	inboxPath := filepath.Join(root, filepath.FromSlash(relPath))
	if _, err := os.Stat(inboxPath); !os.IsNotExist(err) {
		t.Fatalf("intent still at source %s", relPath)
	}
}
