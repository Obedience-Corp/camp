package workitem

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/charmbracelet/x/ansi"
)

func doctorExampleFindings() []docFinding {
	findings := []docFinding{{Code: codeProjectNotFound, Severity: docSeverityWarning,
		Target: "workitem:workflow/design/color-profiles", Message: "projects entry projects/immerse does not exist",
		FixHint: "Verify whether the project was renamed or removed; --fix does not remove this reference."}}
	for _, name := range []string{"commerce", "keys", "release", "release-fix"} {
		findings = append(findings, docFinding{Code: codeScopeNotLocal, Severity: docSeverityWarning,
			Target: "link:lnk_" + name, Message: "scope path projects/worktrees/activity/" + name + " is not on this machine",
			FixHint:   "remove only if finished: camp workitem unlink --id lnk_" + name,
			scopeKind: links.ScopeWorktree, scopeProject: "projects/timeline", scopePath: "projects/worktrees/activity/" + name})
	}
	return append(findings, docFinding{Code: codeWorktreeGone, Severity: docSeverityInfo, Target: "registry", worktreeCount: 12})
}

func TestDoctorHumanGroupsReviewAndExpectedState(t *testing.T) {
	var out bytes.Buffer
	if err := emitDocHuman(&out, doctorExampleFindings(), 80, false, true); err != nil {
		t.Fatal(err)
	}
	text := ansi.Strip(out.String())
	for _, want := range []string{"0 errors, 5 warnings, 1 informational summary", "Needs review", "Unavailable worktrees: 4 links", "Parent project also unavailable: projects/timeline", "No action needed", "12 worktree links preserved", "No automatic repairs available for the remaining findings.", "camp workitem doctor --verbose"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for _, f := range doctorExampleFindings() {
		if strings.Contains(text, f.Code) || strings.Contains(text, "lnk_") {
			t.Errorf("internal identifiers clutter default report:\n%s", text)
		}
		if f.scopePath != "" && !strings.Contains(text, f.scopePath) {
			t.Errorf("lost affected path %q", f.scopePath)
		}
	}
	if strings.Count(text, "Parent project also unavailable:") != 1 {
		t.Errorf("repeated parent context:\n%s", text)
	}
}

func TestDoctorHumanWrapsNarrowAndUnicodePaths(t *testing.T) {
	findings := doctorExampleFindings()
	findings[0].Target = "workitem:workflow/design/" + strings.Repeat("长い名前", 25)
	for _, width := range []int{40, 60, 80, 120} {
		var out bytes.Buffer
		if err := emitDocHuman(&out, findings, width, true, false); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Errorf("width %d: got %d columns: %q", width, got, line)
			}
		}
	}
}

func TestDoctorHumanVerboseRetainsDiagnostics(t *testing.T) {
	var out bytes.Buffer
	findings := doctorExampleFindings()
	if err := emitDocHuman(&out, findings, 0, true, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		for _, want := range []string{f.Code, f.Target, f.Message, f.FixHint} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("missing verbose detail %q", want)
			}
		}
	}
}

func TestDoctorHumanRepairAndErrorStates(t *testing.T) {
	cases := []struct {
		name         string
		findings     []docFinding
		fix          bool
		want, absent string
	}{
		{name: "clean", want: "No issues found.", absent: "--fix"},
		{name: "repairable info", findings: []docFinding{{Code: codeWorktreeProjectUnknown, Severity: docSeverityInfo, AutoFixable: true, Message: "Record project"}}, want: "1 automatic repair available: camp workitem doctor --fix", absent: "No action needed"},
		{name: "pending repair", findings: []docFinding{{Severity: docSeverityWarning, AutoFixable: true, Message: "Repair failed"}}, fix: true, want: "1 automatic repair still pending", absent: "No automatic repairs available"},
		{name: "fatal", findings: []docFinding{{Code: codeRegistryParseError, Severity: docSeverityError, Message: "invalid YAML", AutoFixable: true}}, want: "Errors", absent: "No issues found"},
		{name: "manual", findings: []docFinding{{Severity: docSeverityWarning, Message: "Missing project"}}, want: "--fix cannot resolve them automatically", absent: "No issues found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := emitDocHuman(&out, tc.findings, 0, false, tc.fix); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.absent) {
				t.Fatalf("unexpected report:\n%s", out.String())
			}
		})
	}
}

func TestDoctorGroupsKeepDifferentProjectsSeparate(t *testing.T) {
	findings := doctorExampleFindings()[1:5]
	findings[1].scopeProject = "projects/another"
	findings[2].scopeProject = ""
	groups := doctorGroups(findings)
	if len(groups) != 3 || len(groups[0]) != 2 {
		t.Fatalf("unexpected grouping: %+v", groups)
	}
}

func TestDoctorPresentationContextDoesNotChangeJSON(t *testing.T) {
	var out bytes.Buffer
	if err := emitDocJSON(&out, doctorExampleFindings()); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Findings     []map[string]json.RawMessage `json:"findings"`
		WarningCount int                          `json:"warning_count"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 6 || report.WarningCount != 5 {
		t.Fatalf("JSON findings were grouped: %s", out.String())
	}
	allowed := map[string]bool{"code": true, "severity": true, "target": true, "message": true, "fix_hint": true, "auto_fixable": true}
	for _, f := range report.Findings {
		for key := range f {
			if !allowed[key] {
				t.Errorf("presentation field leaked into JSON: %s", key)
			}
		}
	}
}

type doctorFailWriter struct{ err error }

func (w doctorFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDoctorHumanPropagatesWriteFailure(t *testing.T) {
	want := errors.New("output closed")
	if err := emitDocHuman(doctorFailWriter{want}, doctorExampleFindings(), 80, false, true); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestDoctorHumanCountsLinksWhenWorktreesAreShared(t *testing.T) {
	findings := doctorExampleFindings()[1:3]
	findings[1].scopePath = findings[0].scopePath
	var out bytes.Buffer
	if err := emitDocHuman(&out, findings, 0, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Unavailable worktrees: 2 links") {
		t.Fatalf("must count links, not unique worktrees: %s", out.String())
	}
}
