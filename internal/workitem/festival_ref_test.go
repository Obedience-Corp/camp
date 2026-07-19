package workitem

import "testing"

func TestFestivalRefFromID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare id", "SC0001", "SC0001"},
		{"dir name with trailing id", "sync-clone-transport-SC0001", "SC0001"},
		{"campaign-relative festival path", "festivals/planning/sync-clone-transport-SC0001", "SC0001"},
		{"empty", "", ""},
		{"non-alphanumeric yields no ref", "weird_slug!", ""},
		{"whitespace trimmed", "  SC0001  ", "SC0001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FestivalRefFromID(tt.in); got != tt.want {
				t.Fatalf("FestivalRefFromID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFestivalRef(t *testing.T) {
	tests := []struct {
		name string
		wi   *WorkItem
		want string
	}{
		{"nil", nil, ""},
		{
			"festival with source id",
			&WorkItem{WorkflowType: WorkflowTypeFestival, SourceID: "SC0001"},
			"SC0001",
		},
		{
			"festival falls back to relative path",
			&WorkItem{WorkflowType: WorkflowTypeFestival, RelativePath: "festivals/planning/sync-clone-transport-SC0001"},
			"SC0001",
		},
		{
			"design workitem is not a festival",
			&WorkItem{WorkflowType: WorkflowTypeDesign, SourceID: "whatever", StableID: "design-x"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FestivalRef(tt.wi); got != tt.want {
				t.Fatalf("FestivalRef() = %q, want %q", got, tt.want)
			}
		})
	}
}
