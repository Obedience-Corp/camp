package intent

import (
	"errors"
	"fmt"
	"regexp"
)

// Validation errors.
var (
	ErrIDRequired        = errors.New("id is required")
	ErrTitleRequired     = errors.New("title is required")
	ErrTitleTooShort     = errors.New("title must be at least 3 characters")
	ErrStatusRequired    = errors.New("status is required")
	ErrCreatedAtRequired = errors.New("created_at is required")
	ErrInvalidIDFormat   = errors.New("id does not match required format slug-YYYYMMDD-HHMMSS")
	ErrInvalidStatus     = errors.New("invalid status")
	ErrInvalidType       = errors.New("invalid type")
	ErrInvalidPriority   = errors.New("invalid priority")
	ErrInvalidHorizon    = errors.New("invalid horizon")
	ErrPromotedToStatus  = errors.New("promoted_to can only be set when status is done")
)

// intentIDPattern matches the expected ID format: slug-YYYYMMDD-HHMMSS.
// The slug is optional (for empty titles), but when present must be lowercase alphanumeric with hyphens.
var intentIDPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?-)?\d{8}-\d{6}$`)

// Validate checks the intent for consistency and completeness.
// Returns a slice of all validation errors found.
func (i *Intent) Validate() []error {
	var errs []error

	// Required fields
	if i.ID == "" {
		errs = append(errs, ErrIDRequired)
	} else if !intentIDPattern.MatchString(i.ID) {
		errs = append(errs, fmt.Errorf("%w: got %q", ErrInvalidIDFormat, i.ID))
	}

	if i.Title == "" {
		errs = append(errs, ErrTitleRequired)
	} else if len(i.Title) < 3 {
		errs = append(errs, ErrTitleTooShort)
	}

	if i.Status == "" {
		errs = append(errs, ErrStatusRequired)
	} else if !isValidStatus(i.Status) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidStatus, i.Status))
	}

	if i.CreatedAt.IsZero() {
		errs = append(errs, ErrCreatedAtRequired)
	}

	// Optional enum field validation
	if i.Type != "" && !isValidType(i.Type) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidType, i.Type))
	}

	if i.Priority != "" && !isValidPriority(i.Priority) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidPriority, i.Priority))
	}

	if i.Horizon != "" && !isValidHorizon(i.Horizon) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidHorizon, i.Horizon))
	}

	// Consistency validation
	if i.PromotedTo != "" && i.Status != StatusDone {
		errs = append(errs, ErrPromotedToStatus)
	}

	return errs
}

// isValidStatus returns true if the status is a known valid value.
func isValidStatus(s Status) bool {
	switch s {
	case StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled:
		return true
	default:
		return false
	}
}

// isValidType returns true if the type is a known valid value.
func isValidType(t Type) bool {
	switch t {
	case TypeIdea, TypeFeature, TypeBug, TypeResearch, TypeChore, TypeFeedback:
		return true
	default:
		return false
	}
}

// isValidPriority returns true if the priority is a known valid value.
func isValidPriority(p Priority) bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh:
		return true
	default:
		return false
	}
}

// isValidHorizon returns true if the horizon is a known valid value.
func isValidHorizon(h Horizon) bool {
	switch h {
	case HorizonNow, HorizonNext, HorizonLater, HorizonSomeday:
		return true
	default:
		return false
	}
}

// IsValid returns true if the intent passes all validation checks.
func (i *Intent) IsValid() bool {
	return len(i.Validate()) == 0
}
