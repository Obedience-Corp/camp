package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/spf13/cobra"
)

// What the listing says about a job, and what it tells the user to do next.
//
// This is the surface the incident was reported from: a job that had been
// wedged for fifty minutes rendered the same word as one three seconds in, so
// the listing described the queue accurately and told the user nothing. Each
// case below is a state that needs a different decision, and the assertion is
// that the row and the footer name it.
func TestRenderJobsHuman(t *testing.T) {
	t.Parallel()

	running := jobs.Job{ID: "job-running", Seq: 1, Kind: jobs.KindCommitTree, AutoWrite: true}
	failed := jobs.Job{ID: "job-failed", Seq: 2, Kind: jobs.KindCommitTree, Attempts: 1,
		LastError: "the commit message writer (ob commit) did not finish within 5m0s"}

	tests := []struct {
		name         string
		entries      []jobs.Entry
		wantContains []string
		wantOmits    []string
	}{
		{
			name:    "an empty queue says so and offers nothing",
			entries: nil,
			wantContains: []string{
				"No deferred commits queued.",
			},
			wantOmits: []string{"stalled", "camp jobs"},
		},
		{
			// A healthy running job still reports its clock, because "running"
			// alone is the word that hid the incident.
			name: "a running job reports how long the attempt has been going",
			entries: []jobs.Entry{{
				Job: running, State: "running", Lane: ".", RunningFor: 3 * time.Second,
			}},
			wantContains: []string{"running", "running 3s", "CREATED", "AGE"},
			wantOmits:    []string{"stalled"},
		},
		{
			name: "created column shows the absolute enqueue time",
			entries: []jobs.Entry{{
				Job: jobs.Job{
					ID: "job-created", Seq: 1, Kind: jobs.KindCommitTree,
					CreatedAt: "2026-07-28T11:30:00.000Z",
				},
				State: "pending", Lane: ".",
			}},
			wantContains: []string{
				"CREATED",
				jobs.FormatCreated("2026-07-28T11:30:00.000Z"),
			},
		},
		{
			name: "a job past the writer budget is stalled, and says by how much",
			entries: []jobs.Entry{{
				Job: running, State: "running", Lane: ".", RunningFor: 51 * time.Minute,
				Stalled: true, StalledReason: "writer running 51m, budget 5m",
			}},
			wantContains: []string{
				"stalled",
				"writer running 51m, budget 5m",
				// The one command that ends it, because nothing else will:
				// the lane has a live worker, so the queue reads it as progress.
				"camp jobs drop --running <id>",
				"your next commit picks them up",
			},
		},
		{
			// The other stall takes the opposite advice, so the footers must
			// not be interchangeable.
			name: "a job with no live worker is stalled and waiting for one",
			entries: []jobs.Entry{{
				Job: running, State: "running", Lane: ".", RunningFor: time.Second,
				Stuck: true, Stalled: true, StalledReason: "no live worker",
			}},
			wantContains: []string{
				"stalled",
				"no live worker",
				"camp jobs run",
			},
			wantOmits: []string{"camp jobs drop --running"},
		},
		{
			name: "a failed job carries the reason it failed",
			entries: []jobs.Entry{{
				Job: failed, State: "failed", Lane: ".",
			}},
			wantContains: []string{
				"failed after 1 attempt",
				"did not finish within 5m0s",
				"camp jobs retry all",
			},
			wantOmits: []string{"gave up after", "of 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			renderJobsHuman(cmd, tt.entries, map[string]bool{})

			got := buf.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("listing omits %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.wantOmits {
				if strings.Contains(got, unwanted) {
					t.Errorf("listing contains %q, which does not apply here:\n%s", unwanted, got)
				}
			}
		})
	}
}
