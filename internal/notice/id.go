package notice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
)

// A notice about one subject gets an ID built from its kind and a short hash
// of that subject, so the ID a user types stays short however long the subject
// is, and a newly declared root still produces a new signature that notifies
// even after an older one was dismissed. The subject itself travels on
// Notice.Subject for any surface that wants to show it.
const (
	KindNeverSynced   = "never-synced"
	KindManifestDrift = "manifest-drift"
)

// subjectHashLen is the number of hex digits of the subject hash an ID keeps.
const subjectHashLen = 6

// SubjectID returns the ID of a kind's notice about subject.
func SubjectID(kind, subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return kind + "-" + hex.EncodeToString(sum[:])[:subjectHashLen]
}

// legacyKinds maps the ID prefixes camp v0.10.0 wrote, which carried the whole
// subject, to the kind each became. Dismissals are committed, so ids in this
// form are already in users' .campaign/notices.yaml.
var legacyKinds = []struct {
	prefix string
	kind   string
}{
	{prefix: "artifact-root-never-synced:", kind: KindNeverSynced},
	{prefix: "artifact-manifest-drift:", kind: KindManifestDrift},
}

// CanonicalID maps an ID in its v0.10.0 long form to the current one and
// returns any other ID unchanged.
func CanonicalID(id string) string {
	for _, l := range legacyKinds {
		subject, ok := strings.CutPrefix(id, l.prefix)
		if ok && subject != "" {
			return SubjectID(l.kind, artifacts.NormalizeRootPath(subject))
		}
	}
	return id
}

// kindSummaries describe each notice in a few words, for surfaces that show an
// ID without the notice that produced it.
var kindSummaries = map[string]string{
	KindNeverSynced:   "declared artifact root that has never synced to another machine",
	KindManifestDrift: "artifact root that drifted from its committed manifest",
	missingRootID:     "declared artifact roots that are not on this machine",
	DungeonLegacyID:   "camp that uses the visible dungeon/ layout",
	StaleLinksID:      "workitem links that point at paths that no longer exist",
}

// subjectKinds are the kinds whose IDs carry a subject hash.
var subjectKinds = []string{KindNeverSynced, KindManifestDrift}

// subjectKind returns the kind of an ID that carries a subject hash.
func subjectKind(id string) (string, bool) {
	for _, kind := range subjectKinds {
		if strings.HasPrefix(id, kind+"-") {
			return kind, true
		}
	}
	return "", false
}

// HasSubject reports whether an ID names a notice about one subject.
func HasSubject(id string) bool {
	_, ok := subjectKind(id)
	return ok
}

// Summary describes the notice an ID belongs to, or "" for an ID camp does not
// produce.
func Summary(id string) string {
	if s, ok := kindSummaries[id]; ok {
		return s
	}
	if kind, ok := subjectKind(id); ok {
		return kindSummaries[kind]
	}
	return ""
}

// Subjects maps the ID of every per-root notice the declared roots could raise
// to its root, so a surface listing IDs can name what each one is about. The
// hash in an ID is one-way; recomputing it over the declared roots is how the
// subject comes back. A root that is no longer declared has no entry.
func Subjects(campaignRoot string) map[string]string {
	subjects := map[string]string{}
	cfg, err := artifacts.Load(campaignRoot)
	if err != nil {
		return subjects
	}
	for _, root := range cfg.Roots {
		rel := artifacts.NormalizeRootPath(root.Path)
		if rel == "" {
			continue
		}
		subjects[SubjectID(KindNeverSynced, rel)] = rel
		subjects[SubjectID(KindManifestDrift, rel)] = rel
	}
	return subjects
}
