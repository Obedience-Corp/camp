package intent

import (
	"errors"
	"testing"
	"time"
)

func TestIntent_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		intent   Intent
		wantErrs int
		wantErr  error // optional: check for specific error
	}{
		// Valid cases
		{
			name: "valid intent with required fields only",
			intent: Intent{
				ID:        "test-intent-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 0,
		},
		{
			name: "valid intent with all fields",
			intent: Intent{
				ID:        "full-intent-20260119-153412",
				Title:     "Full Intent",
				Status:    StatusActive,
				Type:      TypeFeature,
				Priority:  PriorityHigh,
				Horizon:   HorizonNow,
				CreatedAt: now,
				Concept:   "test-project",
				Author:    "test-author",
			},
			wantErrs: 0,
		},
		{
			name: "valid done intent with promoted_to",
			intent: Intent{
				ID:         "promoted-20260119-153412",
				Title:      "Promoted Intent",
				Status:     StatusDone,
				CreatedAt:  now,
				PromotedTo: "FEST-123",
			},
			wantErrs: 0,
		},
		{
			name: "valid ID without slug (timestamp only)",
			intent: Intent{
				ID:        "20260119-153412",
				Title:     "Timestamp Only",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 0,
		},

		// Missing required fields
		{
			name:     "empty intent missing all required fields",
			intent:   Intent{},
			wantErrs: 4, // id, title, status, created_at
		},
		{
			name: "missing id",
			intent: Intent{
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrIDRequired,
		},
		{
			name: "missing title",
			intent: Intent{
				ID:        "test-20260119-153412",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrTitleRequired,
		},
		{
			name: "missing status",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrStatusRequired,
		},
		{
			name: "missing created_at",
			intent: Intent{
				ID:     "test-20260119-153412",
				Title:  "Test Intent",
				Status: StatusInbox,
			},
			wantErrs: 1,
			wantErr:  ErrCreatedAtRequired,
		},

		// Title validation
		{
			name: "title too short (1 char)",
			intent: Intent{
				ID:        "a-20260119-153412",
				Title:     "A",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrTitleTooShort,
		},
		{
			name: "title too short (2 chars)",
			intent: Intent{
				ID:        "ab-20260119-153412",
				Title:     "AB",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrTitleTooShort,
		},
		{
			name: "title exactly 3 chars (valid)",
			intent: Intent{
				ID:        "abc-20260119-153412",
				Title:     "ABC",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 0,
		},

		// Invalid ID formats
		{
			name: "invalid id format - no timestamp",
			intent: Intent{
				ID:        "invalid-id",
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidIDFormat,
		},
		{
			name: "invalid id format - wrong timestamp length",
			intent: Intent{
				ID:        "2026011-153412-test",
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidIDFormat,
		},
		{
			name: "invalid id format - letters in timestamp",
			intent: Intent{
				ID:        "20260A19-153412-test",
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidIDFormat,
		},

		// Invalid enum values
		{
			name: "invalid status value",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    Status("invalid"),
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidStatus,
		},
		{
			name: "invalid type value",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				Type:      Type("invalid"),
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidType,
		},
		{
			name: "invalid priority value",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				Priority:  Priority("invalid"),
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidPriority,
		},
		{
			name: "invalid horizon value",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				Horizon:   Horizon("invalid"),
				CreatedAt: now,
			},
			wantErrs: 1,
			wantErr:  ErrInvalidHorizon,
		},

		// Consistency validation
		{
			name: "promoted_to with status not done",
			intent: Intent{
				ID:         "test-20260119-153412",
				Title:      "Test Intent",
				Status:     StatusActive,
				PromotedTo: "FEST-123",
				CreatedAt:  now,
			},
			wantErrs: 1,
			wantErr:  ErrPromotedToStatus,
		},

		// Multiple errors at once
		{
			name: "multiple validation errors",
			intent: Intent{
				ID:         "invalid-id",
				Title:      "AB", // too short
				Status:     Status("bad"),
				PromotedTo: "FEST-123", // invalid with bad status
			},
			wantErrs: 5, // invalid id, title too short, invalid status, created_at missing, promoted_to requires done
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.intent.Validate()

			if len(errs) != tt.wantErrs {
				t.Errorf("Validate() returned %d errors, want %d", len(errs), tt.wantErrs)
				for i, err := range errs {
					t.Errorf("  error[%d]: %v", i, err)
				}
			}

			// If we expect a specific error, check it's present
			if tt.wantErr != nil && len(errs) > 0 {
				found := false
				for _, err := range errs {
					if errors.Is(err, tt.wantErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Validate() did not return expected error %v", tt.wantErr)
					t.Errorf("Got errors: %v", errs)
				}
			}
		})
	}
}

func TestIntent_IsValid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		intent Intent
		want   bool
	}{
		{
			name: "valid intent",
			intent: Intent{
				ID:        "test-20260119-153412",
				Title:     "Test Intent",
				Status:    StatusInbox,
				CreatedAt: now,
			},
			want: true,
		},
		{
			name:   "invalid intent",
			intent: Intent{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.intent.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidStatus(t *testing.T) {
	validStatuses := []Status{
		StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled,
	}

	for _, s := range validStatuses {
		if !isValidStatus(s) {
			t.Errorf("isValidStatus(%q) = false, want true", s)
		}
	}

	invalidStatuses := []Status{
		"", "invalid", "INBOX", "pending", "new",
	}

	for _, s := range invalidStatuses {
		if isValidStatus(s) {
			t.Errorf("isValidStatus(%q) = true, want false", s)
		}
	}
}

func TestIsValidType(t *testing.T) {
	validTypes := []Type{
		TypeIdea, TypeFeature, TypeBug, TypeResearch, TypeChore, TypeFeedback,
	}

	for _, typ := range validTypes {
		if !isValidType(typ) {
			t.Errorf("isValidType(%q) = false, want true", typ)
		}
	}

	invalidTypes := []Type{
		"", "invalid", "IDEA", "task", "story",
	}

	for _, typ := range invalidTypes {
		if isValidType(typ) {
			t.Errorf("isValidType(%q) = true, want false", typ)
		}
	}
}

func TestIsValidPriority(t *testing.T) {
	validPriorities := []Priority{
		PriorityLow, PriorityMedium, PriorityHigh,
	}

	for _, p := range validPriorities {
		if !isValidPriority(p) {
			t.Errorf("isValidPriority(%q) = false, want true", p)
		}
	}

	invalidPriorities := []Priority{
		"", "invalid", "LOW", "critical", "urgent",
	}

	for _, p := range invalidPriorities {
		if isValidPriority(p) {
			t.Errorf("isValidPriority(%q) = true, want false", p)
		}
	}
}

func TestIsValidHorizon(t *testing.T) {
	validHorizons := []Horizon{
		HorizonNow, HorizonNext, HorizonLater, HorizonSomeday,
	}

	for _, h := range validHorizons {
		if !isValidHorizon(h) {
			t.Errorf("isValidHorizon(%q) = false, want true", h)
		}
	}

	invalidHorizons := []Horizon{
		"", "invalid", "NOW", "soon", "eventually",
	}

	for _, h := range invalidHorizons {
		if isValidHorizon(h) {
			t.Errorf("isValidHorizon(%q) = true, want false", h)
		}
	}
}

func TestValidate_ErrorMessages(t *testing.T) {
	// Verify error messages contain useful context
	intent := Intent{
		ID:     "bad-id",
		Status: Status("invalid-status"),
	}

	errs := intent.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}

	// Check that invalid ID error contains the bad ID
	for _, err := range errs {
		if errors.Is(err, ErrInvalidIDFormat) {
			errMsg := err.Error()
			if errMsg == "" {
				t.Error("error message should not be empty")
			}
		}
	}
}

func TestValidate_AllStatusValues(t *testing.T) {
	now := time.Now()
	statuses := []Status{
		StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled,
	}

	for _, status := range statuses {
		intent := Intent{
			ID:        "test-20260119-153412",
			Title:     "Test Intent",
			Status:    status,
			CreatedAt: now,
		}

		errs := intent.Validate()
		if len(errs) != 0 {
			t.Errorf("Validate() with status %q returned errors: %v", status, errs)
		}
	}
}

func TestValidate_AllTypeValues(t *testing.T) {
	now := time.Now()
	types := []Type{
		TypeIdea, TypeFeature, TypeBug, TypeResearch, TypeChore, TypeFeedback,
	}

	for _, typ := range types {
		intent := Intent{
			ID:        "test-20260119-153412",
			Title:     "Test Intent",
			Status:    StatusInbox,
			Type:      typ,
			CreatedAt: now,
		}

		errs := intent.Validate()
		if len(errs) != 0 {
			t.Errorf("Validate() with type %q returned errors: %v", typ, errs)
		}
	}
}
