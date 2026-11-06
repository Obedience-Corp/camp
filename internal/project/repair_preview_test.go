package project

import "testing"

func TestNormalizeRepairOrigin(t *testing.T) {
	tests := []struct{ input, want string }{
		{"git@github.com:Obedience-Corp/obey-voice.git", "github.com/obedience-corp/obey-voice"},
		{"https://github.com/Obedience-Corp/obey-voice.git/", "github.com/obedience-corp/obey-voice"},
		{"ssh://git@github.com/Obedience-Corp/obey-voice", "github.com/obedience-corp/obey-voice"},
		{"git@github.com:someone/obey-voice.git", "github.com/someone/obey-voice"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeRepairOrigin(tt.input); got != tt.want {
			t.Errorf("NormalizeRepairOrigin(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRepairSelectionHint(t *testing.T) {
	if got := RepairSelectionHint(&RepairPreview{}); got == "" {
		t.Fatal("empty discovery must offer explicit path")
	}
	if got := RepairSelectionHint(&RepairPreview{Candidates: []RepairCandidate{{Path: "/repo"}}}); got == "" {
		t.Fatal("one candidate still requires explicit selection")
	}
	if got := RepairSelectionHint(&RepairPreview{Candidates: []RepairCandidate{{Path: "/a"}, {Path: "/b"}}}); got == "" {
		t.Fatal("ambiguous candidates need selection")
	}
}
