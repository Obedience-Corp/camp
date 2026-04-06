package priority

import "testing"

func TestRank(t *testing.T) {
	tests := []struct {
		name string
		p    ManualPriority
		want int
	}{
		{"high", High, 1},
		{"medium", Medium, 2},
		{"low", Low, 3},
		{"none", None, 4},
		{"unknown", ManualPriority("critical"), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Rank(); got != tt.want {
				t.Errorf("ManualPriority(%q).Rank() = %d, want %d", tt.p, got, tt.want)
			}
		})
	}
}

func TestRank_Ordering(t *testing.T) {
	if !(High.Rank() < Medium.Rank()) {
		t.Error("High.Rank() must be less than Medium.Rank()")
	}
	if !(Medium.Rank() < Low.Rank()) {
		t.Error("Medium.Rank() must be less than Low.Rank()")
	}
	if !(Low.Rank() < None.Rank()) {
		t.Error("Low.Rank() must be less than None.Rank()")
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		p    ManualPriority
		want bool
	}{
		{"high", High, true},
		{"medium", Medium, true},
		{"low", Low, true},
		{"none", None, false},
		{"unknown", ManualPriority("critical"), false},
		{"empty_string", ManualPriority(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Valid(); got != tt.want {
				t.Errorf("ManualPriority(%q).Valid() = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}
