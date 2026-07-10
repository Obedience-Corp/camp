package quest

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// Checklist storage constants.
const (
	// ChecklistFileName is the sibling file that holds a quest's checklist.
	ChecklistFileName = "checklist.yaml"
	// ChecklistSchemaV1 is the current checklist schema version.
	ChecklistSchemaV1 = "quest-checklist/v1alpha1"
	// ChecklistItemIDPrefix prefixes every checklist item id.
	ChecklistItemIDPrefix = "qci_"

	// rankStep is the gap left between consecutive item ranks so items can be
	// reordered without renumbering the whole list.
	rankStep = 10
	// idHexLen is the random suffix length for checklist item ids.
	idHexLen = 6
)

const hexAlphabet = "0123456789abcdef"

// ChecklistItemStatus is the lifecycle state of a single checklist item.
type ChecklistItemStatus string

const (
	ItemOpen    ChecklistItemStatus = "open"
	ItemDoing   ChecklistItemStatus = "doing"
	ItemDone    ChecklistItemStatus = "done"
	ItemDropped ChecklistItemStatus = "dropped"
)

// Valid reports whether the status is supported.
func (s ChecklistItemStatus) Valid() bool {
	switch s {
	case ItemOpen, ItemDoing, ItemDone, ItemDropped:
		return true
	default:
		return false
	}
}

// Terminal reports whether the status closes the item (done or dropped).
func (s ChecklistItemStatus) Terminal() bool {
	return s == ItemDone || s == ItemDropped
}

// ChecklistItemStatuses returns every supported item status.
func ChecklistItemStatuses() []ChecklistItemStatus {
	return []ChecklistItemStatus{ItemOpen, ItemDoing, ItemDone, ItemDropped}
}

// ParseChecklistItemStatus converts a raw flag value into an item status.
func ParseChecklistItemStatus(raw string) (ChecklistItemStatus, error) {
	status := ChecklistItemStatus(strings.TrimSpace(strings.ToLower(raw)))
	if status.Valid() {
		return status, nil
	}
	return "", ErrInvalidChecklistStatus
}

var (
	ErrChecklistItemNotFound  = camperrors.Wrap(camperrors.ErrNotFound, "checklist item not found")
	ErrChecklistItemAmbiguous = camperrors.Wrap(camperrors.ErrInvalidInput, "checklist item selector is ambiguous")
	ErrInvalidChecklistStatus = camperrors.Wrap(camperrors.ErrInvalidInput, "checklist item status is invalid")
	ErrEmptyChecklistTitle    = camperrors.Wrap(camperrors.ErrInvalidInput, "checklist item title is required")
	ErrChecklistItemIDFailed  = camperrors.Wrap(camperrors.ErrInvalidInput, "could not allocate a unique checklist item id")
)

// ChecklistWorkitem is a stored reference to a workitem that backs an item.
// The id is the source of truth; the on-disk path is resolved at read time so
// moving a workitem to the dungeon never invalidates the checklist row.
type ChecklistWorkitem struct {
	ID  string `yaml:"id" json:"id"`
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// ChecklistItem is one ordered unit of work under a quest.
type ChecklistItem struct {
	ID          string              `yaml:"id" json:"id"`
	Title       string              `yaml:"title" json:"title"`
	Status      ChecklistItemStatus `yaml:"status" json:"status"`
	Rank        int                 `yaml:"rank" json:"rank"`
	Workitem    *ChecklistWorkitem  `yaml:"workitem,omitempty" json:"workitem,omitempty"`
	Notes       string              `yaml:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt   time.Time           `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time           `yaml:"updated_at" json:"updated_at"`
	CompletedAt *time.Time          `yaml:"completed_at,omitempty" json:"completed_at,omitempty"`
}

// Checklist is the thin, quest-owned board of work units.
type Checklist struct {
	SchemaVersion string          `yaml:"schema_version" json:"schema_version"`
	QuestID       string          `yaml:"quest_id" json:"quest_id"`
	Items         []ChecklistItem `yaml:"items" json:"items"`
}

// ChecklistPathForQuest returns the checklist file path for a resolved quest.
func ChecklistPathForQuest(q *Quest) string {
	return filepath.Join(filepath.Dir(q.Path), ChecklistFileName)
}

// LoadChecklist reads a quest's checklist. A missing file is not an error: a
// quest without a checklist yields an empty checklist stamped with questID.
func LoadChecklist(ctx context.Context, path, questID string) (*Checklist, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Checklist{SchemaVersion: ChecklistSchemaV1, QuestID: questID}, nil
		}
		return nil, camperrors.Wrapf(err, "read checklist %s", path)
	}
	var cl Checklist
	if err := yaml.Unmarshal(data, &cl); err != nil {
		return nil, camperrors.Wrapf(err, "parse checklist %s", path)
	}
	if cl.SchemaVersion == "" {
		cl.SchemaVersion = ChecklistSchemaV1
	}
	if cl.QuestID == "" {
		cl.QuestID = questID
	}
	cl.Sort()
	return &cl, nil
}

// SaveChecklist writes a checklist to disk atomically, sorted by rank.
func SaveChecklist(ctx context.Context, path string, cl *Checklist) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cl == nil {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "checklist is nil")
	}
	if cl.SchemaVersion == "" {
		cl.SchemaVersion = ChecklistSchemaV1
	}
	cl.Sort()
	data, err := yaml.Marshal(cl)
	if err != nil {
		return camperrors.Wrapf(err, "marshal checklist for quest %s", cl.QuestID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return camperrors.Wrap(err, "create quest directory")
	}
	if err := fsutil.WriteFileAtomically(path, data, 0644); err != nil {
		return camperrors.Wrapf(err, "write checklist %s", path)
	}
	return nil
}

// Sort orders items by rank, then creation time, then id for stability.
func (cl *Checklist) Sort() {
	slices.SortFunc(cl.Items, func(a, b ChecklistItem) int {
		if a.Rank != b.Rank {
			return a.Rank - b.Rank
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.Before(b.CreatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Resolve finds an item by exact id, unique id suffix, or unique substring.
func (cl *Checklist) Resolve(selector string) (*ChecklistItem, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, ErrChecklistItemNotFound
	}
	for i := range cl.Items {
		if cl.Items[i].ID == selector {
			return &cl.Items[i], nil
		}
	}
	var matches []*ChecklistItem
	for i := range cl.Items {
		if strings.HasSuffix(cl.Items[i].ID, selector) || strings.Contains(cl.Items[i].ID, selector) {
			matches = append(matches, &cl.Items[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, camperrors.Wrapf(ErrChecklistItemNotFound, "%q", selector)
	case 1:
		return matches[0], nil
	default:
		return nil, camperrors.Wrapf(ErrChecklistItemAmbiguous, "%q matches %d items", selector, len(matches))
	}
}

// NextRank returns a rank that sorts after every existing item.
func (cl *Checklist) NextRank() int {
	max := 0
	for _, it := range cl.Items {
		if it.Rank > max {
			max = it.Rank
		}
	}
	return max + rankStep
}

// Add appends a new item and returns a pointer to the stored copy.
func (cl *Checklist) Add(item ChecklistItem) *ChecklistItem {
	cl.Items = append(cl.Items, item)
	return &cl.Items[len(cl.Items)-1]
}

// idSet returns the set of item ids currently in the checklist.
func (cl *Checklist) idSet() map[string]bool {
	set := make(map[string]bool, len(cl.Items))
	for _, it := range cl.Items {
		set[it.ID] = true
	}
	return set
}

// GenerateChecklistItemID mints a qci_YYYYMMDD_<hex> id, scanning existing ids
// for collisions and re-rolling the random suffix (workitem id pattern).
func GenerateChecklistItemID(now time.Time, existing map[string]bool) (string, error) {
	prefix := ChecklistItemIDPrefix + now.UTC().Format("20060102") + "_"
	for range 1000 {
		suffix, err := randomHex(idHexLen)
		if err != nil {
			return "", camperrors.Wrap(err, "generate checklist item id")
		}
		id := prefix + suffix
		if !existing[id] {
			return id, nil
		}
	}
	return "", ErrChecklistItemIDFailed
}

func randomHex(n int) (string, error) {
	if n <= 0 {
		return "", nil
	}
	var b strings.Builder
	b.Grow(n)
	limit := big.NewInt(int64(len(hexAlphabet)))
	for range n {
		idx, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		b.WriteByte(hexAlphabet[idx.Int64()])
	}
	return b.String(), nil
}
