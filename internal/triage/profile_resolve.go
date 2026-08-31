package triage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/triage/scaffold"
)

// ProfileNameSweep is the fast pass: metadata evidence everywhere, compact
// review. For when you want the inbox triaged and nothing else.
const ProfileNameSweep = "sweep"

// ProfileNameDeep re-reads every lane properly, for the periodic pass where
// the point is to be right rather than quick.
const ProfileNameDeep = "deep"

// ProfileNames returns the built-in profile vocabulary.
func ProfileNames() []string {
	return []string{ProfileNameDefault, ProfileNameSweep, ProfileNameDeep}
}

// BuiltinProfile returns a named built-in, or an error naming the ones that
// exist.
func BuiltinProfile(name string) (ResolvedProfile, error) {
	profile := DefaultProfile()
	switch name {
	case ProfileNameDefault, "":
	case ProfileNameSweep:
		// Nothing is read deeply and batches are large: a sweep is for
		// clearing volume, and a deep read per row would defeat the purpose.
		for stage := range profile.Evidence.DepthByStage {
			profile.Evidence.DepthByStage[stage] = EvidenceDepthMetadata
		}
		profile.Review.BatchSize = 20
		profile.Routing.EvidenceTier = RoutingTierCheap
	case ProfileNameDeep:
		for stage := range profile.Evidence.DepthByStage {
			profile.Evidence.DepthByStage[stage] = EvidenceDepthDeep
		}
		profile.Review.BatchSize = 3
		profile.Routing.EvidenceTier = RoutingTierStrong
	default:
		return ResolvedProfile{}, &ValidationError{
			Kind: "profile",
			Violations: []Violation{{
				Field:   "profile",
				Message: "unknown profile " + quote(name),
				Allowed: ProfileNames(),
			}},
		}
	}
	profile.Normalize()
	return profile, nil
}

// ProfileResolution is a resolved profile and where it came from.
type ProfileResolution struct {
	// Name is the campaign file's path when one was used, else the built-in
	// profile's name.
	Name    string
	Profile ResolvedProfile
	// FromFile reports whether `.campaign/triage/profile.yaml` was read.
	FromFile bool
	// TypePolicies are the resolved per-type policies, keyed by type.
	TypePolicies map[string]TypePolicy
}

// ProfilePath is where a campaign's profile lives.
func ProfilePath(campaignRoot string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(scaffold.DirName), "profile.yaml")
}

// TypePolicyPath is where one type's policy lives.
func TypePolicyPath(campaignRoot, wfType string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(scaffold.DirName),
		"types", wfType+".yaml")
}

// ResolveProfileNamed resolves the profile a run will use.
//
// Order: the campaign's `profile.yaml` when it exists, otherwise the named
// built-in. A named profile passed explicitly wins over the file, because
// `--profile sweep` is a statement about this run rather than about the
// campaign.
//
// Every key the file omits inherits the built-in default, which is why the
// scaffold writes them all out commented: the file is meant to be readable and
// edited down, not filled in from nothing.
func ResolveProfileNamed(ctx context.Context, campaignRoot, name string) (*ProfileResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	base, err := BuiltinProfile(name)
	if err != nil {
		return nil, err
	}

	resolution := &ProfileResolution{Name: nameOrDefault(name), Profile: base}

	// An explicitly named non-default profile is this run's instruction and
	// is not merged with the campaign file.
	if name == "" || name == ProfileNameDefault {
		path := ProfilePath(campaignRoot)
		raw, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			merged, mergeErr := mergeProfileYAML(base, raw, relOf(campaignRoot, path))
			if mergeErr != nil {
				return nil, mergeErr
			}
			resolution.Profile = merged
			resolution.FromFile = true
			resolution.Name = ProfileNameDefault
		case !os.IsNotExist(readErr):
			return nil, camperrors.Wrapf(readErr, "reading %s", path)
		}
	}

	resolution.Profile.Normalize()
	if err := ValidateResolvedProfile(resolution.Profile, resolution.sourceLabel()); err != nil {
		return nil, err
	}

	policies, err := resolveTypePolicies(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}
	resolution.TypePolicies = policies
	return resolution, nil
}

// sourceLabel names the file or built-in an error should point at.
func (r *ProfileResolution) sourceLabel() string {
	if r.FromFile {
		return scaffold.DirName + "/profile.yaml"
	}
	return "built-in profile " + quote(r.Name)
}

// mergeProfileYAML decodes a campaign profile over a base, strictly.
//
// Strict decoding is the point: a typo'd key that silently did nothing would
// be worse than an error, because the operator would believe they had changed
// behavior they had not.
func mergeProfileYAML(base ResolvedProfile, raw []byte, label string) (ResolvedProfile, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	merged := base
	if err := dec.Decode(&merged); err != nil {
		// An empty file decodes to EOF and means "use the defaults", which is
		// a legal way to say it — deleting every key is exactly what the
		// scaffold's own comment invites. Returning the base rather than the
		// zero value is the difference between that and a profile with no
		// values at all.
		if decErr := profileDecodeError(label, err); decErr != nil {
			return ResolvedProfile{}, decErr
		}
		return base, nil
	}
	return merged, nil
}

// profileDecodeError turns a yaml error into camp's validation shape, keeping
// the file name in front of the operator.
func profileDecodeError(label string, err error) error {
	message := strings.TrimSpace(err.Error())
	if message == "EOF" {
		// An empty file means "use the defaults", which is legal.
		return nil
	}
	return &ValidationError{
		Kind: "profile",
		Violations: []Violation{{
			Field:   label,
			Message: message,
		}},
	}
}

// ValidateResolvedProfile checks every enum and bound, naming the source.
func ValidateResolvedProfile(profile ResolvedProfile, source string) error {
	var violations []Violation

	add := func(field string, found []Violation) {
		for _, v := range found {
			v.Field = source + ": " + field
			violations = append(violations, v)
		}
	}

	add("runs.mode", checkEnum("runs.mode", string(profile.Runs.Mode), RunModes()))
	add("review.group_by", checkEnum("review.group_by",
		string(profile.Review.GroupBy), ReviewGroupBys()))
	add("review.approval", checkEnum("review.approval",
		string(profile.Review.Approval), ApprovalGranules()))
	add("preflight.identity", checkEnum("preflight.identity",
		string(profile.Preflight.Identity), IdentityPolicies()))

	for _, tier := range []struct {
		field string
		value RoutingTier
	}{
		{"routing.evidence_tier", profile.Routing.EvidenceTier},
		{"routing.escalation_tier", profile.Routing.EscalationTier},
		{"routing.synthesis_tier", profile.Routing.SynthesisTier},
	} {
		add(tier.field, checkEnum(tier.field, string(tier.value), RoutingTiers()))
	}

	for _, stage := range sortedKeys(profile.Evidence.DepthByStage) {
		field := "evidence.depth_by_stage." + stage
		add(field, checkEnum(field, string(profile.Evidence.DepthByStage[stage]), EvidenceDepths()))
	}

	if profile.Review.BatchSize < 1 {
		violations = append(violations, Violation{
			Field:   source + ": review.batch_size",
			Message: "must be at least 1",
		})
	}
	if profile.Runs.StaleAfterDays < 0 {
		violations = append(violations, Violation{
			Field:   source + ": runs.stale_after_days",
			Message: "must not be negative",
		})
	}
	if profile.Anchors.RecheckMinutes < 0 {
		violations = append(violations, Violation{
			Field:   source + ": anchors.recheck_minutes",
			Message: "must not be negative (0 disables caching)",
		})
	}

	// An export path that escapes the campaign would write outside the tree
	// the run is describing.
	if export := profile.Outputs.PrioritiesExport; export != "" {
		if filepath.IsAbs(export) || export == ".." || strings.HasPrefix(export, "../") {
			violations = append(violations, Violation{
				Field:   source + ": outputs.priorities_export",
				Message: "must be a camp-relative path inside the camp, got " + quote(export),
			})
		}
	}

	return newValidationError("profile", violations)
}

// resolveTypePolicies reads every `types/*.yaml` the campaign ships.
func resolveTypePolicies(ctx context.Context, campaignRoot string) (map[string]TypePolicy, error) {
	dir := filepath.Join(campaignRoot, filepath.FromSlash(scaffold.DirName), "types")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]TypePolicy{}, nil
		}
		return nil, camperrors.Wrapf(err, "reading %s", dir)
	}

	out := make(map[string]TypePolicy, len(entries))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wfType := strings.TrimSuffix(name, ".yaml")
		policy, err := loadTypePolicy(filepath.Join(dir, name),
			scaffold.DirName+"/types/"+name, wfType)
		if err != nil {
			return nil, err
		}
		out[wfType] = policy
	}
	return out, nil
}

// loadTypePolicy decodes one type policy over its built-in default.
func loadTypePolicy(path, label, wfType string) (TypePolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TypePolicy{}, camperrors.Wrapf(err, "reading %s", path)
	}

	// Decode twice: once over the built-in so scalar keys inherit, and once
	// into an empty policy to see what the file actually declared.
	//
	// yaml merges maps, so a decode over the built-in would union the
	// vocabularies — and a type policy that can only ever ADD labels cannot
	// express the thing types exist for. design.yaml offering four
	// dispositions means four, not four plus the six it inherited.
	policy := TypePolicyFor(wfType)
	if err := decodeStrict(raw, &policy, label); err != nil {
		return TypePolicy{}, err
	}

	var declared TypePolicy
	if err := decodeStrict(raw, &declared, label); err != nil {
		return TypePolicy{}, err
	}
	if len(declared.Dispositions) > 0 {
		policy.Dispositions = declared.Dispositions
	}
	if err := ValidateTypePolicy(policy, label); err != nil {
		return TypePolicy{}, err
	}
	return policy, nil
}

// decodeStrict decodes yaml into target, refusing unknown keys.
func decodeStrict(raw []byte, target any, label string) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(target); err != nil {
		return profileDecodeError(label, err)
	}
	return nil
}

// ValidateTypePolicy checks a policy's enums and, critically, that every label
// it offers maps to an action camp can actually perform.
//
// A vocabulary label that resolves to nothing is the failure that would
// otherwise surface at apply time, on a verdict someone already approved.
func ValidateTypePolicy(policy TypePolicy, source string) error {
	var violations []Violation

	for _, v := range checkEnum("evidence", string(policy.Evidence), EvidenceDepths()) {
		v.Field = source + ": evidence"
		violations = append(violations, v)
	}
	for _, v := range checkOptionalEnum("routing_tier", string(policy.RoutingTier), RoutingTiers()) {
		v.Field = source + ": routing_tier"
		violations = append(violations, v)
	}

	actions := CanonicalActions()
	for _, label := range policy.Labels() {
		action := string(policy.Dispositions[label])
		if !containsString(actions, action) {
			violations = append(violations, Violation{
				Field:   source + ": dispositions." + label,
				Message: "maps to " + quote(action) + ", which is not an action camp can perform",
				Allowed: actions,
			})
		}
	}

	if len(policy.Dispositions) == 0 {
		violations = append(violations, Violation{
			Field:   source + ": dispositions",
			Message: "is required: a type with no vocabulary can never be decided",
		})
	}

	return newValidationError("type policy", violations)
}

// containsString reports membership.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// nameOrDefault is the profile name to record.
func nameOrDefault(name string) string {
	if name == "" {
		return ProfileNameDefault
	}
	return name
}

// relOf renders a path relative to the campaign for diagnostics.
func relOf(campaignRoot, path string) string {
	if rel, err := filepath.Rel(campaignRoot, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}
