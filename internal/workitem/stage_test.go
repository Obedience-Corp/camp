package workitem

import "testing"

func TestValidStagesForType_CustomTypeAllowsActive(t *testing.T) {
	stages := ValidStagesForType(WorkflowType("feature"))
	found := false
	for _, s := range stages {
		if s == LifecycleStageActive {
			found = true
		}
	}
	if !found {
		t.Fatalf("ValidStagesForType(feature) = %#v, want active included (custom workitems are active by location)", stages)
	}
}

func TestIsValidStageForTypes(t *testing.T) {
	cases := []struct {
		name  string
		stage LifecycleStage
		types []string
		want  bool
	}{
		{"design active", LifecycleStageActive, []string{"design"}, true},
		{"explore active", LifecycleStageActive, []string{"explore"}, true},
		{"custom type active", LifecycleStageActive, []string{"feature"}, true},
		{"custom type none", LifecycleStageNone, []string{"feature"}, true},
		{"custom type inbox invalid", LifecycleStageInbox, []string{"feature"}, false},
		{"intent inbox", LifecycleStageInbox, []string{"intent"}, true},
		{"intent active", LifecycleStageActive, []string{"intent"}, true},
		{"intent planning invalid", LifecycleStagePlanning, []string{"intent"}, false},
		{"no type filter active", LifecycleStageActive, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidStageForTypes(tc.stage, tc.types); got != tc.want {
				t.Errorf("IsValidStageForTypes(%q, %v) = %v, want %v", tc.stage, tc.types, got, tc.want)
			}
		})
	}
}
