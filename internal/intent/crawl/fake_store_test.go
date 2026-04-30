package crawl

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// fakeStore is an in-memory IntentStore for unit tests. It mirrors
// the public surface needed by the crawl package without touching
// the filesystem.
//
// All methods record their calls so tests can assert the exact
// audit/move sequence performed by the runner.
type fakeStore struct {
	intents map[string]*intent.Intent

	// Behavior overrides
	moveErr     error
	saveErr     error
	listErr     error
	findErr     error
	moveErrFor  map[string]error // per-id move error
	saveErrFor  map[string]error
	moveStubFor map[string]intent.Status // change destination for id

	// Call records
	saveCalls []*intent.Intent
	moveCalls []moveCall
}

type moveCall struct {
	ID     string
	Status intent.Status
}

func newFakeStore(seed ...*intent.Intent) *fakeStore {
	store := &fakeStore{
		intents:     make(map[string]*intent.Intent, len(seed)),
		moveErrFor:  map[string]error{},
		saveErrFor:  map[string]error{},
		moveStubFor: map[string]intent.Status{},
	}
	for _, in := range seed {
		store.intents[in.ID] = cloneIntent(in)
	}
	return store
}

func cloneIntent(in *intent.Intent) *intent.Intent {
	out := *in
	return &out
}

func (s *fakeStore) List(_ context.Context, opts *intent.ListOptions) ([]*intent.Intent, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var status *intent.Status
	if opts != nil {
		status = opts.Status
	}
	var out []*intent.Intent
	for _, in := range s.intents {
		if status != nil && in.Status != *status {
			continue
		}
		out = append(out, cloneIntent(in))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStore) Find(_ context.Context, id string) (*intent.Intent, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	in, ok := s.intents[id]
	if !ok {
		return nil, errors.New("intent not found: " + id)
	}
	return cloneIntent(in), nil
}

func (s *fakeStore) Save(_ context.Context, in *intent.Intent) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if e, ok := s.saveErrFor[in.ID]; ok {
		return e
	}
	s.saveCalls = append(s.saveCalls, cloneIntent(in))
	s.intents[in.ID] = cloneIntent(in)
	return nil
}

func (s *fakeStore) Move(_ context.Context, id string, newStatus intent.Status) (*intent.Intent, error) {
	if s.moveErr != nil {
		return nil, s.moveErr
	}
	if e, ok := s.moveErrFor[id]; ok {
		return nil, e
	}
	target := newStatus
	if stub, ok := s.moveStubFor[id]; ok {
		target = stub
	}
	in, ok := s.intents[id]
	if !ok {
		return nil, errors.New("intent not found: " + id)
	}
	in.Status = target
	in.UpdatedAt = time.Now()
	in.Path = ".campaign/intents/" + string(target) + "/" + id + ".md"
	s.moveCalls = append(s.moveCalls, moveCall{ID: id, Status: target})
	return cloneIntent(in), nil
}

func (s *fakeStore) Count(_ context.Context) ([]intent.StatusCount, int, error) {
	statuses := intent.AllStatuses()
	counts := make([]intent.StatusCount, 0, len(statuses))
	total := 0
	for _, st := range statuses {
		n := 0
		for _, in := range s.intents {
			if in.Status == st {
				n++
			}
		}
		counts = append(counts, intent.StatusCount{Status: st, Count: n})
		total += n
	}
	return counts, total, nil
}
