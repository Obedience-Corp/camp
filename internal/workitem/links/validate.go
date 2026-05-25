package links

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathsafe"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// ValidationError captures one rule failure. Validate returns a slice; the
// CLI surface promotes the slice to a single wrapped camperrors error.
type ValidationError struct {
	LinkID  string // empty for top-level errors
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.LinkID == "" {
		return fmt.Sprintf("validation error on %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error on %s (link %s): %s", e.Field, e.LinkID, e.Message)
}

// ValidateOptions controls which checks Validate performs.
type ValidateOptions struct {
	// CampaignRoot enables filesystem-backed checks (path containment, target
	// existence). Required for the safety bounds documented in SCHEMA.md §8.
	CampaignRoot string

	// AllowMissing skips the "scope.path target must exist" check. Used by
	// migrations.
	AllowMissing bool

	// WorkitemIDs is the set of known workitem ids on disk. When non-nil
	// Validate ensures each link.workitem_id is in the set. Pass nil to skip
	// (used by tests that don't have a campaign on disk).
	WorkitemIDs map[string]struct{}

	// Now is the reference time for created_at skew checks. Defaults to
	// time.Now() if zero.
	Now time.Time
}

var linkIDRegex = regexp.MustCompile(LinkIDPattern)

const (
	maxClockSkewReject = 24 * time.Hour
	maxCreatedByLen    = 64
	maxWorkitemIDLen   = 200
	maxScopePathLen    = 4096
)

var createdByRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Validate returns the list of rule failures for a links registry. An empty
// slice means the registry is valid. Errors are returned in the order
// fields appear in SCHEMA.md §9.
func Validate(ctx context.Context, l *Links, opts ValidateOptions) []ValidationError {
	if err := ctx.Err(); err != nil {
		return []ValidationError{{Field: "context", Message: err.Error()}}
	}
	if l == nil {
		return []ValidationError{{Field: "links", Message: "nil registry"}}
	}

	var errs []ValidationError
	if l.Version != LinksSchemaVersion {
		errs = append(errs, ValidationError{
			Field:   "version",
			Message: "expected " + LinksSchemaVersion + ", got " + l.Version,
		})
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	seenIDs := make(map[string]struct{}, len(l.Links))
	primaryKey := func(s LinkScope) string { return string(s.Kind) + "::" + s.Path }
	seenPrimary := make(map[string]string, len(l.Links))

	for _, link := range l.Links {
		errs = append(errs, validateOneLink(link, opts, now, seenIDs, seenPrimary, primaryKey)...)
	}
	return errs
}

func validateOneLink(link Link, opts ValidateOptions, now time.Time,
	seenIDs map[string]struct{}, seenPrimary map[string]string,
	primaryKey func(LinkScope) string,
) []ValidationError {
	var errs []ValidationError
	addErr := func(field, msg string) {
		errs = append(errs, ValidationError{LinkID: link.ID, Field: field, Message: msg})
	}

	if link.ID == "" {
		addErr("id", "required")
	} else if !linkIDRegex.MatchString(link.ID) {
		addErr("id", "must match "+LinkIDPattern)
	} else if _, dup := seenIDs[link.ID]; dup {
		addErr("id", "duplicate id within registry")
	} else {
		seenIDs[link.ID] = struct{}{}
	}

	if link.WorkitemID == "" {
		addErr("workitem_id", "required")
	} else if len(link.WorkitemID) > maxWorkitemIDLen {
		addErr("workitem_id", fmt.Sprintf("must be at most %d chars", maxWorkitemIDLen))
	} else if err := pathsafe.ValidateSegment("workitem_id", link.WorkitemID); err != nil {
		addErr("workitem_id", err.Error())
	} else if opts.WorkitemIDs != nil {
		if _, known := opts.WorkitemIDs[link.WorkitemID]; !known && !opts.AllowMissing {
			addErr("workitem_id", "no workitem with id "+link.WorkitemID+" found on disk")
		}
	}

	if !isValidScopeKind(link.Scope.Kind) {
		addErr("scope.kind", "unknown scope kind: "+string(link.Scope.Kind))
	}
	if link.Scope.Path == "" {
		addErr("scope.path", "required")
	} else if len(link.Scope.Path) > maxScopePathLen {
		addErr("scope.path", fmt.Sprintf("must be at most %d chars", maxScopePathLen))
	} else if strings.HasPrefix(link.Scope.Path, "/") {
		addErr("scope.path", "must be campaign-relative (no leading /)")
	} else if strings.Contains(link.Scope.Path, "..") {
		addErr("scope.path", "must not contain ..")
	} else if opts.CampaignRoot != "" && !opts.AllowMissing {
		if err := quest.ValidateLinkPath(opts.CampaignRoot, link.Scope.Path); err != nil {
			addErr("scope.path", err.Error())
		}
	}
	if msg, ok := checkKindPathPrefix(link.Scope); !ok {
		addErr("scope.path", msg)
	}

	if !isValidRole(link.Role) {
		addErr("role", "unknown role: "+string(link.Role))
	} else if link.Role == RolePrimary {
		key := primaryKey(link.Scope)
		if otherID, dup := seenPrimary[key]; dup {
			addErr("role", "duplicate primary for scope "+string(link.Scope.Kind)+":"+link.Scope.Path+
				" (also id "+otherID+")")
		} else {
			seenPrimary[key] = link.ID
		}
	}

	if link.CreatedAt.IsZero() {
		addErr("created_at", "required")
	} else {
		skew := now.Sub(link.CreatedAt)
		if skew < 0 {
			skew = -skew
		}
		if skew > maxClockSkewReject {
			addErr("created_at", "more than 24h from now (possible clock skew or wrong year)")
		}
	}

	if link.CreatedBy == "" {
		addErr("created_by", "required")
	} else if len(link.CreatedBy) > maxCreatedByLen {
		addErr("created_by", fmt.Sprintf("must be at most %d chars", maxCreatedByLen))
	} else if !createdByRegex.MatchString(link.CreatedBy) {
		addErr("created_by", "must match [A-Za-z0-9_-]+")
	}

	return errs
}

func isValidScopeKind(k ScopeKind) bool {
	for _, valid := range ValidScopeKinds {
		if k == valid {
			return true
		}
	}
	return false
}

func isValidRole(r Role) bool {
	for _, valid := range ValidRoles {
		if r == valid {
			return true
		}
	}
	return false
}

// checkKindPathPrefix enforces the kind-to-path table in SCHEMA.md §5.
// Returns (errorMessage, ok). ok=true means the scope is acceptable.
func checkKindPathPrefix(s LinkScope) (string, bool) {
	switch s.Kind {
	case ScopeProject:
		if !strings.HasPrefix(s.Path, "projects/") {
			return "scope kind project requires path under projects/", false
		}
		if strings.HasPrefix(s.Path, "projects/worktrees/") {
			return "scope kind project must not be under projects/worktrees/ (use kind worktree)", false
		}
	case ScopeWorktree:
		if !strings.HasPrefix(s.Path, "projects/worktrees/") {
			return "scope kind worktree requires path under projects/worktrees/", false
		}
	case ScopeFestival:
		if !strings.HasPrefix(s.Path, "festivals/") {
			return "scope kind festival requires path under festivals/", false
		}
	case ScopeRepo, ScopeCampaignPath:
		// No prefix constraint.
	}
	return "", true
}

// newValidation wraps a single field/message into the package error type
// used throughout the loader/saver code.
func newValidation(field, message string) error {
	return camperrors.NewValidation(field, message, nil)
}
