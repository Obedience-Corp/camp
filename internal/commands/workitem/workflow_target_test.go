package workitem

import (
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func TestParseWorkflowTarget(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "campaign")
	resolver := paths.NewResolver(root, config.DefaultCampaignPaths())
	cases := []struct {
		name     string
		target   string
		wantPath string
		wantType string
		wantErr  bool
	}{
		{name: "relative design", target: "workflow/design/auth", wantPath: "workflow/design/auth", wantType: "design"},
		{name: "absolute inside campaign", target: filepath.Join(root, "workflow", "explore", "agents"), wantPath: "workflow/explore/agents", wantType: "explore"},
		{name: "outside campaign", target: filepath.Join(root, "..", "other", "workflow", "design", "auth"), wantErr: true},
		{name: "parent escape", target: "../workflow/design/auth", wantErr: true},
		{name: "missing directory segment", target: "workflow/design", wantErr: true},
		{name: "too deep", target: "workflow/design/auth/notes", wantErr: true},
		{name: "intent control area", target: "workflow/intent/auth", wantErr: true},
		{name: "festival control area", target: "workflow/festival/auth", wantErr: true},
		{name: "dungeon control area", target: "workflow/dungeon/auth", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPath, gotType, err := parseWorkflowTarget(root, resolver, tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseWorkflowTarget(%q) error = nil", tc.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWorkflowTarget(%q) error = %v", tc.target, err)
			}
			if gotPath != tc.wantPath || gotType != tc.wantType {
				t.Fatalf("parseWorkflowTarget(%q) = (%q, %q), want (%q, %q)", tc.target, gotPath, gotType, tc.wantPath, tc.wantType)
			}
		})
	}
}

func TestWorkflowScanClassification(t *testing.T) {
	typeCases := []struct {
		name, typeName string
		isDir, want    bool
	}{
		{name: "design", typeName: "design", isDir: true, want: true},
		{name: "custom", typeName: "feature", isDir: true, want: true},
		{name: "intent skipped", typeName: "intent", isDir: true},
		{name: "festival skipped", typeName: "festival", isDir: true},
		{name: "dungeon skipped", typeName: "dungeon", isDir: true},
		{name: "hidden skipped", typeName: ".hidden", isDir: true},
		{name: "file skipped", typeName: "design", isDir: false},
	}
	for _, tc := range typeCases {
		t.Run("type "+tc.name, func(t *testing.T) {
			if got := workflowTypeScannable(tc.typeName, tc.isDir); got != tc.want {
				t.Fatalf("workflowTypeScannable(%q, %v) = %v, want %v", tc.typeName, tc.isDir, got, tc.want)
			}
		})
	}

	childCases := []struct {
		name, childName              string
		isDir, docType, marker, want bool
	}{
		{name: "design unmarked included", childName: "auth", isDir: true, docType: true, want: true},
		{name: "custom marked included", childName: "auth", isDir: true, marker: true, want: true},
		{name: "custom unmarked excluded", childName: "auth", isDir: true},
		{name: "dungeon excluded", childName: "dungeon", isDir: true, docType: true},
		{name: "hidden excluded", childName: ".hidden", isDir: true, docType: true},
	}
	for _, tc := range childCases {
		t.Run("child "+tc.name, func(t *testing.T) {
			if got := workflowChildScannable(tc.childName, tc.isDir, tc.docType, tc.marker); got != tc.want {
				t.Fatalf("workflowChildScannable(%q, %v, %v, %v) = %v, want %v", tc.childName, tc.isDir, tc.docType, tc.marker, got, tc.want)
			}
		})
	}
}

func TestRepairCommandForShellQuotesPath(t *testing.T) {
	got := repairCommandFor("workflow/design/my feature's plan")
	want := "camp workitem repair 'workflow/design/my feature'\\''s plan'"
	if got != want {
		t.Fatalf("repairCommandFor() = %q, want %q", got, want)
	}
}
