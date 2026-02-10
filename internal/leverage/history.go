package leverage

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// HistoryPoint represents aggregate leverage data at a single point in time.
type HistoryPoint struct {
	Date      time.Time            `json:"date"`
	Projects  map[string]*Snapshot `json:"projects"`
	Aggregate *LeverageScore       `json:"aggregate,omitempty"`
	TotalCode int                  `json:"total_code"`
	TotalCost float64              `json:"total_cost"`
}

// LoadHistory loads and aggregates leverage data across projects over the given time range.
// For each calendar week in [since, until], it finds the most recent snapshot on or before
// that date for each project and computes aggregate metrics.
func LoadHistory(ctx context.Context, store SnapshotStorer, projects []string, actualPeople int, since, until time.Time) ([]HistoryPoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load all snapshots for each project upfront (sorted by date)
	projectSnapshots := make(map[string][]*Snapshot)
	for _, proj := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshots, err := store.LoadAll(ctx, proj)
		if err != nil {
			return nil, fmt.Errorf("loading snapshots for %s: %w", proj, err)
		}
		sort.Slice(snapshots, func(i, j int) bool {
			return snapshots[i].Date < snapshots[j].Date
		})
		projectSnapshots[proj] = snapshots
	}

	// Iterate calendar weeks from since to until
	var points []HistoryPoint
	current := weekMonday(since)

	for !current.After(until) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		point := HistoryPoint{
			Date:     current,
			Projects: make(map[string]*Snapshot),
		}

		var scores []*LeverageScore
		var totalCode int
		var totalCost float64

		for _, proj := range projects {
			snap := findMostRecent(projectSnapshots[proj], current)
			if snap == nil {
				continue
			}
			point.Projects[proj] = snap
			totalCode += snap.TotalLines
			if snap.SCC != nil {
				totalCost += snap.SCC.EstimatedCost
			}
			if snap.Leverage != nil {
				scores = append(scores, snap.Leverage)
			}
		}

		// Compute aggregate leverage
		elapsed := ElapsedMonths(since, current)
		if len(scores) > 0 && elapsed > 0 {
			point.Aggregate = AggregateScores(scores, actualPeople, elapsed)
		}

		point.TotalCode = totalCode
		point.TotalCost = totalCost
		points = append(points, point)

		current = current.AddDate(0, 0, 7)
	}

	return points, nil
}

// findMostRecent returns the most recent snapshot on or before the target date.
// Snapshots must be sorted by date ascending. Returns nil if no snapshot exists
// on or before the target date.
func findMostRecent(snapshots []*Snapshot, target time.Time) *Snapshot {
	targetStr := target.Format("2006-01-02")
	var best *Snapshot
	for _, s := range snapshots {
		if s.Date <= targetStr {
			best = s
		} else {
			break
		}
	}
	return best
}
