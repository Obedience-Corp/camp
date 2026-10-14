//go:build container_fs

package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

func TestAddLinked_ConcurrentAutomaticNames(t *testing.T) {
	for _, sameSource := range []bool{true, false} {
		t.Run(fmt.Sprintf("same-source=%t", sameSource), func(t *testing.T) {
			root := t.TempDir()
			writeTestCampaignConfig(t, root)
			const callers = 8
			sources := make([]string, callers)
			results := make([]*LinkResult, callers)
			errs := make([]error, callers)
			for i := range callers {
				if sameSource && i > 0 {
					sources[i] = sources[0]
					continue
				}
				sources[i] = filepath.Join(t.TempDir(), "app")
				if err := os.Mkdir(sources[i], 0o755); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range callers {
				wg.Go(func() {
					<-start
					results[i], errs[i] = AddLinked(context.Background(), root, sources[i], LinkOptions{Name: "app", UniqueName: true})
				})
			}
			close(start)
			wg.Wait()
			created := 0
			for i, err := range errs {
				if err != nil {
					var already *ErrProjectAlreadyLinked
					if !sameSource || !errors.As(err, &already) || already.ExistingName != "app" {
						t.Fatalf("caller %d: %v", i, err)
					}
				} else {
					created++
					linkedInfo, err := os.Stat(filepath.Join(root, results[i].Path))
					if err != nil {
						t.Fatalf("caller %d linked target: %v", i, err)
					}
					sourceInfo, err := os.Stat(sources[i])
					if err != nil || !os.SameFile(linkedInfo, sourceInfo) {
						t.Fatalf("caller %d link does not resolve to source %q: %v", i, sources[i], err)
					}
				}
				marker, err := campaign.ReadMarker(sources[i])
				if err != nil || marker.ActiveCampaignID != "camp-test" {
					t.Fatalf("caller %d marker = %+v, %v", i, marker, err)
				}
			}
			want := callers
			if sameSource {
				want = 1
			}
			entries, err := os.ReadDir(filepath.Join(root, "projects"))
			if err != nil || len(entries) != want || created != want {
				t.Fatalf("created %d, project entries %d, err %v; want %d", created, len(entries), err, want)
			}
		})
	}
}
