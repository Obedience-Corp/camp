package workitem

import (
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// TestGenerateLinkID_RetriesOnCollision proves the retry loop runs when the
// first crypto/rand draw would have collided with an existing registry entry.
// Pre-fix (no retry) this test fails: generateLinkID returns the same ID
// twice.
func TestGenerateLinkID_RetriesOnCollision(t *testing.T) {
	today := time.Now().UTC().Format("20060102")
	colliding := "lnk_" + today + "_010101"

	registry := &links.Links{Links: []links.Link{{
		ID:         colliding,
		WorkitemID: "test",
		Scope:      links.LinkScope{Kind: links.ScopeProject, Path: "projects/x"},
		Role:       links.RolePrimary,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "test",
	}}}

	call := 0
	prev := readRand
	defer func() { readRand = prev }()
	readRand = func(b []byte) (int, error) {
		call++
		if call == 1 {
			b[0], b[1], b[2] = 0x01, 0x01, 0x01
			return 3, nil
		}
		b[0], b[1], b[2] = 0xab, 0xcd, 0xef
		return 3, nil
	}

	id, err := generateLinkID(registry)
	if err != nil {
		t.Fatalf("generateLinkID: %v", err)
	}
	if id == colliding {
		t.Fatalf("expected retry to produce non-colliding ID, got duplicate %q", id)
	}
	if !strings.HasSuffix(id, "_abcdef") {
		t.Errorf("expected retry to use second rand draw (abcdef), got %q", id)
	}
	if call != 2 {
		t.Errorf("expected exactly 2 rand draws (collide, succeed), got %d", call)
	}
}

func TestGenerateLinkID_ReturnsWrappedErrorOnRandFailure(t *testing.T) {
	prev := readRand
	defer func() { readRand = prev }()
	readRand = func(b []byte) (int, error) { return 0, errRandStub }

	_, err := generateLinkID(&links.Links{})
	if err == nil {
		t.Fatal("expected error when readRand fails")
	}
	if !strings.Contains(err.Error(), "generate link id") {
		t.Errorf("error should be wrapped with 'generate link id' context, got: %v", err)
	}
}

var errRandStub = errStub("rand source unavailable")

type errStub string

func (e errStub) Error() string { return string(e) }
