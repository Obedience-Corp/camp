package clone

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/peer"
)

const testPeerRoot = "/home/peer/campaigns/demo"

// report frames lines the way the peer script does, so fixtures describe only
// the interesting part.
func report(lines ...string) string {
	var b strings.Builder
	b.WriteString(quiescenceBeginMarker + "\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString(quiescenceEndMarker + "\n")
	return b.String()
}

// line builds one tab-separated repo line.
func line(repo, status, busy, head string) string {
	return strings.Join([]string{repo, status, busy, head}, "\t")
}

const (
	shaRoot = "1111111111111111111111111111111111111111"
	shaSub  = "2222222222222222222222222222222222222222"
)

func TestParseQuiescenceRejectsUntrustworthyOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		// wantMentions are substrings the error must carry so an operator can
		// tell which repository the report died on.
		wantMentions []string
	}{
		{
			name:         "empty output",
			out:          "",
			wantMentions: []string{"begin marker missing"},
		},
		{
			name:         "login banner without a report",
			out:          "Welcome to peerbox\nLast login: today\n",
			wantMentions: []string{"begin marker missing"},
		},
		{
			name:         "begin marker only",
			out:          quiescenceBeginMarker + "\n",
			wantMentions: []string{"ended before any repo verdict"},
		},
		{
			name:         "stream cut between repo lines",
			out:          quiescenceBeginMarker + "\n" + line(".", "clean", "-", shaRoot) + "\n",
			wantMentions: []string{"ended after repo", `"."`, "without the end marker"},
		},
		{
			name:         "first repo line truncated mid-line",
			out:          quiescenceBeginMarker + "\nprojects/camp\tclean\n",
			wantMentions: []string{"projects/camp", "carried 2 fields, want 4"},
		},
		{
			name: "later repo line truncated mid-line",
			out: quiescenceBeginMarker + "\n" + line(".", "clean", "-", shaRoot) +
				"\nprojects/camp\tclean\t-\n",
			wantMentions: []string{"projects/camp", "carried 3 fields, want 4"},
		},
		{
			name:         "truncated line with no repo name names the previous repo",
			out:          quiescenceBeginMarker + "\n" + line(".", "clean", "-", shaRoot) + "\n\tclean\n",
			wantMentions: []string{`after repo "."`, "carried 2 fields, want 4"},
		},
		{
			name:         "line carrying extra fields",
			out:          report(line(".", "clean", "-", shaRoot) + "\textra"),
			wantMentions: []string{"carried 5 fields, want 4"},
		},
		{
			name:         "repo path escaping the campaign root",
			out:          report(line("../../etc", "clean", "-", shaSub)),
			wantMentions: []string{"unusable repo path", "../../etc"},
		},
		{
			name:         "absolute repo path",
			out:          report(line("/etc/passwd", "clean", "-", shaSub)),
			wantMentions: []string{"unusable repo path", "/etc/passwd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verdicts, err := parseQuiescence([]byte(tt.out), testPeerRoot)
			if err == nil {
				t.Fatalf("parseQuiescence() error = nil, want a protocol error; verdicts = %+v", verdicts)
			}
			if verdicts != nil {
				t.Errorf("parseQuiescence() returned %d verdicts alongside an error; want none", len(verdicts))
			}
			if !errors.Is(err, ErrQuiescenceProtocol) {
				t.Errorf("parseQuiescence() error = %v, want it to match ErrQuiescenceProtocol", err)
			}
			for _, want := range tt.wantMentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("parseQuiescence() error = %q, want it to mention %q", err.Error(), want)
				}
			}
		})
	}
}

func TestParseQuiescenceVerdicts(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []RepoVerdict
	}{
		{
			name: "quiescent campaign root only",
			out:  report(line(".", "clean", "-", shaRoot)),
			want: []RepoVerdict{{Repo: ".", Quiescent: true, HeadSHA: shaRoot}},
		},
		{
			name: "quiescent root and submodule",
			out: report(
				line(".", "clean", "-", shaRoot),
				line("projects/camp", "clean", "-", shaSub),
			),
			want: []RepoVerdict{
				{Repo: ".", Quiescent: true, HeadSHA: shaRoot},
				{Repo: "projects/camp", Quiescent: true, HeadSHA: shaSub},
			},
		},
		{
			name: "one dirty submodule leaves the rest quiescent",
			out: report(
				line(".", "clean", "-", shaRoot),
				line("projects/camp", "dirty", "-", shaSub),
			),
			want: []RepoVerdict{
				{Repo: ".", Quiescent: true, HeadSHA: shaRoot},
				{
					Repo:    "projects/camp",
					Reasons: []string{"uncommitted changes in the working tree"},
					HeadSHA: shaSub,
				},
			},
		},
		{
			name: "stale index.lock disqualifies a clean repo",
			out:  report(line(".", "clean", "index.lock", shaRoot)),
			want: []RepoVerdict{{
				Repo:    ".",
				Reasons: []string{"git operation in progress: index.lock"},
				HeadSHA: shaRoot,
			}},
		},
		{
			name: "mid-rebase repo reports the in-flight operation",
			out:  report(line("projects/camp", "dirty", "rebase-merge,MERGE_HEAD", shaSub)),
			want: []RepoVerdict{{
				Repo: "projects/camp",
				Reasons: []string{
					"uncommitted changes in the working tree",
					"git operation in progress: rebase-merge,MERGE_HEAD",
				},
				HeadSHA: shaSub,
			}},
		},
		{
			name: "unreadable HEAD is disqualifying and carries no sha",
			out:  report(line(".", "clean", "-", "-")),
			want: []RepoVerdict{{Repo: ".", Reasons: []string{"HEAD unreadable"}}},
		},
		{
			name: "uninitialized submodule reports as missing",
			out:  report(line("projects/fest", "missing", "-", "-")),
			want: []RepoVerdict{{
				Repo:    "projects/fest",
				Reasons: []string{"not a git repository on the peer", "HEAD unreadable"},
			}},
		},
		{
			name: "unreportable status is disqualifying",
			out:  report(line("projects/fest", "unknown", "-", shaSub)),
			want: []RepoVerdict{{
				Repo:    "projects/fest",
				Reasons: []string{"peer could not report working-tree status"},
				HeadSHA: shaSub,
			}},
		},
		{
			name: "unrecognized status fails closed",
			out:  report(line(".", "somethingnew", "-", shaRoot)),
			want: []RepoVerdict{{
				Repo:    ".",
				Reasons: []string{"unrecognized working-tree status somethingnew"},
				HeadSHA: shaRoot,
			}},
		},
		{
			name: "login banner before the marker is discarded",
			out:  "MOTD: peerbox\n" + report(line(".", "clean", "-", shaRoot)),
			want: []RepoVerdict{{Repo: ".", Quiescent: true, HeadSHA: shaRoot}},
		},
		{
			name: "crlf line endings parse",
			out: strings.ReplaceAll(
				report(line(".", "clean", "-", shaRoot)), "\n", "\r\n"),
			want: []RepoVerdict{{Repo: ".", Quiescent: true, HeadSHA: shaRoot}},
		},
		{
			name: "blank lines inside the report are ignored",
			out:  report(line(".", "clean", "-", shaRoot), "", line("projects/camp", "clean", "-", shaSub)),
			want: []RepoVerdict{
				{Repo: ".", Quiescent: true, HeadSHA: shaRoot},
				{Repo: "projects/camp", Quiescent: true, HeadSHA: shaSub},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQuiescence([]byte(tt.out), testPeerRoot)
			if err != nil {
				t.Fatalf("parseQuiescence() error = %v, want nil", err)
			}
			assertVerdicts(t, got, tt.want)
		})
	}
}

func assertVerdicts(t *testing.T, got, want []RepoVerdict) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("parseQuiescence() returned %d verdicts, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Repo != w.Repo {
			t.Errorf("verdict[%d].Repo = %q, want %q", i, g.Repo, w.Repo)
		}
		if g.Quiescent != w.Quiescent {
			t.Errorf("verdict[%d] (%s).Quiescent = %v, want %v (reasons: %v)",
				i, g.Repo, g.Quiescent, w.Quiescent, g.Reasons)
		}
		if g.HeadSHA != w.HeadSHA {
			t.Errorf("verdict[%d] (%s).HeadSHA = %q, want %q", i, g.Repo, g.HeadSHA, w.HeadSHA)
		}
		if strings.Join(g.Reasons, "|") != strings.Join(w.Reasons, "|") {
			t.Errorf("verdict[%d] (%s).Reasons = %v, want %v", i, g.Repo, g.Reasons, w.Reasons)
		}
	}
}

func TestQuiescenceReportAggregates(t *testing.T) {
	clean := RepoVerdict{Repo: ".", Quiescent: true, HeadSHA: shaRoot}
	dirty := RepoVerdict{Repo: "projects/camp", Reasons: []string{"uncommitted changes in the working tree"}}

	tests := []struct {
		name          string
		report        *QuiescenceReport
		wantQuiescent bool
		wantFailed    []string
	}{
		{
			name:          "nil report is not quiescent",
			report:        nil,
			wantQuiescent: false,
		},
		{
			name:          "empty report is not quiescent",
			report:        &QuiescenceReport{MachineID: "peerbox"},
			wantQuiescent: false,
		},
		{
			name:          "one non-quiescent repo fails the report",
			report:        &QuiescenceReport{Repos: []RepoVerdict{clean, dirty}},
			wantQuiescent: false,
			wantFailed:    []string{"projects/camp"},
		},
		{
			name:          "all clean passes",
			report:        &QuiescenceReport{Repos: []RepoVerdict{clean}},
			wantQuiescent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.Quiescent(); got != tt.wantQuiescent {
				t.Errorf("Quiescent() = %v, want %v", got, tt.wantQuiescent)
			}
			failed := tt.report.NonQuiescent()
			if len(failed) != len(tt.wantFailed) {
				t.Fatalf("NonQuiescent() returned %d repos, want %d", len(failed), len(tt.wantFailed))
			}
			for i, want := range tt.wantFailed {
				if failed[i].Repo != want {
					t.Errorf("NonQuiescent()[%d].Repo = %q, want %q", i, failed[i].Repo, want)
				}
			}
		})
	}
}

func TestQuiescenceReportVerdictLookup(t *testing.T) {
	r := &QuiescenceReport{Repos: []RepoVerdict{
		{Repo: ".", Quiescent: true, HeadSHA: shaRoot},
		{Repo: "projects/camp", Quiescent: true, HeadSHA: shaSub},
	}}

	tests := []struct {
		name    string
		report  *QuiescenceReport
		repo    string
		wantOK  bool
		wantSHA string
	}{
		{name: "nil report reports nothing verified", report: nil, repo: ".", wantOK: false},
		{name: "unreported repo is not verified", report: r, repo: "projects/fest", wantOK: false},
		{name: "campaign root", report: r, repo: ".", wantOK: true, wantSHA: shaRoot},
		{name: "submodule", report: r, repo: "projects/camp", wantOK: true, wantSHA: shaSub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.report.Verdict(tt.repo)
			if ok != tt.wantOK {
				t.Fatalf("Verdict(%q) ok = %v, want %v", tt.repo, ok, tt.wantOK)
			}
			if got.HeadSHA != tt.wantSHA {
				t.Errorf("Verdict(%q).HeadSHA = %q, want %q", tt.repo, got.HeadSHA, tt.wantSHA)
			}
		})
	}
}

func TestQuiescenceScriptShape(t *testing.T) {
	script := quiescenceScript("/home/peer/it's here")

	tests := []struct {
		name string
		want string
	}{
		{name: "single-quotes the root against shell injection", want: `root='/home/peer/it'\''s here'`},
		{name: "frames the report so banners can be discarded", want: quiescenceBeginMarker},
		{name: "terminates the report so truncation is detectable", want: quiescenceEndMarker},
		{name: "never writes to the peer index", want: "--no-optional-locks"},
		{name: "resolves the real git dir for submodule locks", want: "rev-parse --absolute-git-dir"},
		// Without this guard, rev-parse inside an uninitialized submodule walks
		// up and answers for the campaign root, so an absent submodule inherits
		// the root's clean verdict and HEAD.
		{name: "refuses to resolve a git dir an uninitialized submodule does not own", want: `[ ! -e "$dir/.git" ]`},
		{name: "reports the campaign root repository", want: `camp_report_repo . "$root"`},
		{name: "enumerates submodules from the peer .gitmodules", want: `--get-regexp '^submodule\..*\.path$'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(script, tt.want) {
				t.Errorf("quiescenceScript() missing %q\nscript:\n%s", tt.want, script)
			}
		})
	}
}

func TestCheckQuiescenceRejectsBadInput(t *testing.T) {
	t.Run("nil peer source", func(t *testing.T) {
		if _, err := CheckQuiescence(context.Background(), nil); err == nil {
			t.Fatal("CheckQuiescence(nil) error = nil, want an error")
		}
	})

	t.Run("cancelled context does not dial the peer", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// A non-nil source would otherwise shell out; the context check must
		// short-circuit before any of that.
		_, err := CheckQuiescence(ctx, peer.FromPath("peerbox", testPeerRoot))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CheckQuiescence() error = %v, want context.Canceled", err)
		}
	})
}
