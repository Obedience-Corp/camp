package clone

import "testing"

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		name     string
		severity Severity
		want     string
	}{
		{"info", SeverityInfo, "info"},
		{"warning", SeverityWarning, "warning"},
		{"error", SeverityError, "error"},
		{"unknown", Severity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.want {
				t.Errorf("Severity.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
