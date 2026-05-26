// Package links implements the workitem link registry at
// `.campaign/workitems/links.yaml` plus the local `current.yaml` selection.
//
// Contract: see SCHEMA.md. Types in this file mirror that schema. Validation
// rules live in validate.go. Persistence (with file locking and atomic
// rename) lives in load.go / save.go. The package reuses
// `internal/quest/link.go::ValidateLinkPath` for campaign-root containment.
package links

import (
	"sort"
	"time"
)

// Schema versions are immutable strings persisted in YAML.
const (
	LinksSchemaVersion   = "workitem-links/v1alpha1"
	CurrentSchemaVersion = "workitem-current/v1alpha1"
)

// LinkIDPattern is the regex form of a link ID: `lnk_<yyyymmdd>_<6 hex>`.
// Implementations parse and emit only this form.
const LinkIDPattern = `^lnk_[0-9]{8}_[0-9a-f]{6}$`

// ScopeKind enumerates the scope a link can attach to.
type ScopeKind string

const (
	ScopeProject      ScopeKind = "project"
	ScopeRepo         ScopeKind = "repo"
	ScopeCampaignPath ScopeKind = "campaign_path"
	ScopeFestival     ScopeKind = "festival"
	ScopeWorktree     ScopeKind = "worktree"
)

// ValidScopeKinds is the enumerable set; iteration order is deterministic.
var ValidScopeKinds = []ScopeKind{
	ScopeProject, ScopeRepo, ScopeCampaignPath, ScopeFestival, ScopeWorktree,
}

// Role enumerates the role a link plays in commit routing.
type Role string

const (
	RolePrimary    Role = "primary"
	RoleRelated    Role = "related"
	RoleBlockedBy  Role = "blocked_by"
	RoleSupersedes Role = "supersedes"
)

// ValidRoles is the enumerable set; iteration order is deterministic.
var ValidRoles = []Role{RolePrimary, RoleRelated, RoleBlockedBy, RoleSupersedes}

// LinkScope is the target of a link.
type LinkScope struct {
	Kind ScopeKind `yaml:"kind" json:"kind"`
	Path string    `yaml:"path" json:"path"`
}

// Link is one workitem-to-scope relationship.
type Link struct {
	ID          string    `yaml:"id" json:"id"`
	WorkitemID  string    `yaml:"workitem_id" json:"workitem_id"`
	WorkitemKey string    `yaml:"workitem_key,omitempty" json:"workitem_key,omitempty"`
	Scope       LinkScope `yaml:"scope" json:"scope"`
	Role        Role      `yaml:"role" json:"role"`
	CreatedAt   time.Time `yaml:"created_at" json:"created_at"`
	CreatedBy   string    `yaml:"created_by" json:"created_by"`
}

// Links is the top-level registry persisted to `links.yaml`.
type Links struct {
	Version string `yaml:"version" json:"version"`
	Links   []Link `yaml:"links" json:"links"`
}

// Current is the top-level shape persisted to `current.yaml`.
type Current struct {
	Version    string    `yaml:"version" json:"version"`
	WorkitemID string    `yaml:"workitem_id" json:"workitem_id"`
	SelectedAt time.Time `yaml:"selected_at" json:"selected_at"`
}

// FindByID returns the link with the matching id and true; otherwise nil, false.
func (l *Links) FindByID(id string) (*Link, bool) {
	for i := range l.Links {
		if l.Links[i].ID == id {
			return &l.Links[i], true
		}
	}
	return nil, false
}

// FindByScope returns every link whose scope matches the given kind+path.
// The slice is a new allocation; callers may mutate it without affecting the
// underlying registry.
func (l *Links) FindByScope(kind ScopeKind, path string) []Link {
	var out []Link
	for _, link := range l.Links {
		if link.Scope.Kind == kind && link.Scope.Path == path {
			out = append(out, link)
		}
	}
	return out
}

// PrimaryForScope returns the primary link for (kind, path) and true, or
// nil, false if none is set.
func (l *Links) PrimaryForScope(kind ScopeKind, path string) (*Link, bool) {
	for i := range l.Links {
		link := &l.Links[i]
		if link.Role == RolePrimary && link.Scope.Kind == kind && link.Scope.Path == path {
			return link, true
		}
	}
	return nil, false
}

// AddLink appends link to the registry.
//
//   - If a primary link already exists for (link.Scope, RolePrimary) and the
//     incoming link's role is primary, the call returns a ValidationError
//     unless replace is true. With replace, the existing primary is removed
//     before append.
//   - If a link with the same ID already exists, the call returns a
//     ValidationError unconditionally — IDs are append-only.
//   - Non-primary roles have no uniqueness constraint.
func (l *Links) AddLink(link Link, replace bool) error {
	if _, found := l.FindByID(link.ID); found {
		return newValidation("id", "duplicate link id: "+link.ID)
	}
	if link.Role == RolePrimary {
		if existing, ok := l.PrimaryForScope(link.Scope.Kind, link.Scope.Path); ok {
			if !replace {
				return newValidation("scope",
					"primary link already exists for "+string(link.Scope.Kind)+":"+link.Scope.Path+
						" (id "+existing.ID+"); use --replace to override")
			}
			_ = l.RemoveLinkByID(existing.ID)
		}
	}
	l.Links = append(l.Links, link)
	return nil
}

// RemoveLinkByID drops the link with the given id. Returns true if a link
// was removed, false if no match was found.
func (l *Links) RemoveLinkByID(id string) bool {
	for i := range l.Links {
		if l.Links[i].ID == id {
			l.Links = append(l.Links[:i], l.Links[i+1:]...)
			return true
		}
	}
	return false
}

// Sort orders links by (scope.kind, scope.path, created_at, id) in place.
// The writer calls Sort before marshaling to keep diffs minimal.
func (l *Links) Sort() {
	sort.SliceStable(l.Links, func(i, j int) bool {
		a, b := l.Links[i], l.Links[j]
		if a.Scope.Kind != b.Scope.Kind {
			return a.Scope.Kind < b.Scope.Kind
		}
		if a.Scope.Path != b.Scope.Path {
			return a.Scope.Path < b.Scope.Path
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
}

// Empty returns a freshly-initialized Links with the current schema version
// and a non-nil, empty Links slice (so YAML marshals as `links: []` rather
// than `links: null`).
func Empty() *Links {
	return &Links{Version: LinksSchemaVersion, Links: []Link{}}
}
