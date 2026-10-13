package fresh

import (
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/starter"
)

func camp(id, name string) config.RegisteredCampaign {
	return config.RegisteredCampaign{ID: id, Name: name, Path: "/camps/" + name + "-" + id}
}

func TestDefaultLinkCamp(t *testing.T) {
	tests := []struct {
		name     string
		camps    []config.RegisteredCampaign
		wantID   string
		wantErr  []string
		lacksErr []string
	}{
		{
			name:  "no camps leaves the starter to the caller",
			camps: nil,
		},
		{
			name:   "the camp named after the starter wins",
			camps:  []config.RegisteredCampaign{camp("a1", "alpha"), camp("f1", starter.Name)},
			wantID: "f1",
		},
		{
			name:    "camps without a starter name stop and name --campaign",
			camps:   []config.RegisteredCampaign{camp("a1", "alpha"), camp("b1", "beta")},
			wantErr: []string{"--campaign <name>", "alpha", "beta", "/camps/alpha-a1", `no camp is named "festival"`},
		},
		{
			name:     "two camps with the starter name are not guessed between",
			camps:    []config.RegisteredCampaign{camp("f1", starter.Name), camp("f2", starter.Name), camp("z1", "zeta")},
			wantErr:  []string{"--campaign <id>", "f1", "f2", "more than one camp"},
			lacksErr: []string{"zeta"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultLinkCamp(tt.camps)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("defaultLinkCamp() = %+v, nil; want an error", got)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				for _, lacks := range tt.lacksErr {
					if strings.Contains(err.Error(), lacks) {
						t.Errorf("error %q should not contain %q", err, lacks)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultLinkCamp() error = %v", err)
			}
			switch {
			case tt.wantID == "" && got != nil:
				t.Fatalf("defaultLinkCamp() = %+v; want nil", got)
			case tt.wantID != "" && (got == nil || got.ID != tt.wantID):
				t.Fatalf("defaultLinkCamp() = %+v; want ID %q", got, tt.wantID)
			}
		})
	}
}

// Every caller that could see "not in a camp" before auto-link existed must
// still see it: naming a project, turning linking off, and any detection
// failure other than "no camp here" never reach git or the registry.
func TestPlanAutoLink_KeepsNotInCampError(t *testing.T) {
	other := errors.New("permission denied")
	tests := []struct {
		name      string
		detectErr error
		req       autoLinkRequest
	}{
		{"named project", campaign.ErrNotInCampaign, autoLinkRequest{explicitTarget: true}},
		{"--no-link", campaign.ErrNotInCampaign, autoLinkRequest{noLink: true}},
		{"--no-link with --campaign", campaign.ErrNotInCampaign, autoLinkRequest{noLink: true, campaign: "work"}},
		{"detection failed for another reason", other, autoLinkRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := planAutoLink(t.Context(), tt.detectErr, tt.req)
			if plan != nil {
				t.Fatalf("planAutoLink() plan = %+v; want nil", plan)
			}
			if !errors.Is(err, tt.detectErr) {
				t.Fatalf("planAutoLink() error = %v; want it to wrap %v", err, tt.detectErr)
			}
			if !strings.HasPrefix(err.Error(), "not in a camp") {
				t.Fatalf("planAutoLink() error = %q; want the existing 'not in a camp' prefix", err)
			}
		})
	}
}

func TestAutoLinkPlan_Lines(t *testing.T) {
	repo := unlinkedRepo{top: "/work/myapp", main: "/work/myapp"}
	tests := []struct {
		name           string
		plan           autoLinkPlan
		createdStarter bool
		wantPreview    string
		wantNotice     string
	}{
		{
			name:           "starter camp",
			plan:           autoLinkPlan{repo: repo, name: "myapp", campRoot: "/home/camps/festival", campName: "festival", createStarter: true},
			createdStarter: true,
			wantPreview:    "Would link myapp into your festival camp, creating the camp at /home/camps/festival.",
			wantNotice:     "Linked myapp into your festival camp, created at /home/camps/festival. Undo the link with 'camp project unlink'.",
		},
		{
			name:        "existing camp",
			plan:        autoLinkPlan{repo: repo, name: "myapp", campRoot: "/home/camps/work", campName: "work"},
			wantPreview: "Would link myapp into the work camp at /home/camps/work.",
			wantNotice:  "Linked myapp into the work camp at /home/camps/work. Undo with 'camp project unlink'.",
		},
		{
			name:        "name taken by another project",
			plan:        autoLinkPlan{repo: repo, name: "myapp-2", campRoot: "/home/camps/work", campName: "work"},
			wantPreview: "Would link myapp as myapp-2 into the work camp at /home/camps/work.",
			wantNotice:  "Linked myapp as myapp-2 into the work camp at /home/camps/work. Undo with 'camp project unlink'.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.previewLine(); got != tt.wantPreview {
				t.Errorf("previewLine()\n got: %s\nwant: %s", got, tt.wantPreview)
			}
			if got := tt.plan.notice(tt.createdStarter); got != tt.wantNotice {
				t.Errorf("notice()\n got: %s\nwant: %s", got, tt.wantNotice)
			}
		})
	}
}
