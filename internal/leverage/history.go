package leverage

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// HistoryPeriod controls the aggregation granularity for history output.
type HistoryPeriod string

const (
	// PeriodWeekly shows raw week-to-week deltas.
	PeriodWeekly HistoryPeriod = "weekly"
	// PeriodMonthly groups snapshots by calendar month.
	PeriodMonthly HistoryPeriod = "monthly"
)

// HistoryPoint represents aggregate leverage data at a single point in time.
type HistoryPoint struct {
	Date      time.Time            `json:"date"`
	Projects  map[string]*Snapshot `json:"projects"`
	Aggregate *LeverageScore       `json:"aggregate,omitempty"`
	TotalCode int                  `json:"total_code"`
	TotalCost float64              `json:"total_cost"`

	// Period fields — populated by LoadPeriodHistory
	Period         HistoryPeriod `json:"period,omitempty"`
	DeltaCode      int           `json:"delta_code,omitempty"`
	DeltaEstCost   float64       `json:"delta_est_cost,omitempty"`
	PeriodLeverage float64       `json:"period_leverage,omitempty"`
	IsFirst        bool          `json:"is_first,omitempty"`
	IsNegative     bool          `json:"is_negative,omitempty"`
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

// LoadPeriodHistory loads snapshots and computes period-based leverage deltas.
// Each HistoryPoint represents the change between consecutive snapshots.
// Period controls granularity: weekly returns raw week-to-week deltas,
// monthly groups snapshots by calendar month.
func LoadPeriodHistory(ctx context.Context, store SnapshotStorer, projects []string, actualPeople int, since, until time.Time, period HistoryPeriod) ([]HistoryPoint, error) {
	// Get the weekly cumulative points first
	weekly, err := LoadHistory(ctx, store, projects, actualPeople, since, until)
	if err != nil {
		return nil, err
	}

	if len(weekly) == 0 {
		return nil, nil
	}

	// Compute deltas between consecutive weeks
	weeklyDeltas := computeWeeklyDeltas(weekly, actualPeople)

	if period == PeriodMonthly {
		return aggregateMonthly(weeklyDeltas, actualPeople), nil
	}

	return weeklyDeltas, nil
}

// snapshotAggregate builds an aggregate LeverageScore from the per-project
// snapshots at a single history point, without needing elapsed months.
func snapshotAggregate(point HistoryPoint) *LeverageScore {
	var scores []*LeverageScore
	for _, snap := range point.Projects {
		if snap != nil && snap.Leverage != nil {
			scores = append(scores, snap.Leverage)
		}
	}
	if len(scores) == 0 {
		return nil
	}
	// Use AggregateScores with dummy elapsed (we only need the sums)
	return AggregateScores(scores, 1, 1.0)
}

// computeWeeklyDeltas converts cumulative history points into period deltas.
func computeWeeklyDeltas(points []HistoryPoint, actualPeople int) []HistoryPoint {
	deltas := make([]HistoryPoint, 0, len(points))

	for i, point := range points {
		delta := HistoryPoint{
			Date:      point.Date,
			Projects:  point.Projects,
			Aggregate: point.Aggregate,
			TotalCode: point.TotalCode,
			TotalCost: point.TotalCost,
			Period:    PeriodWeekly,
		}

		if i == 0 {
			delta.IsFirst = true
			delta.DeltaCode = point.TotalCode
			delta.DeltaEstCost = point.TotalCost
			deltas = append(deltas, delta)
			continue
		}

		prev := points[i-1]
		delta.DeltaCode = point.TotalCode - prev.TotalCode
		delta.DeltaEstCost = point.TotalCost - prev.TotalCost

		prevAgg := snapshotAggregate(prev)
		currAgg := snapshotAggregate(point)
		if prevAgg != nil && currAgg != nil {
			periodMonths := ElapsedMonths(prev.Date, point.Date)
			ps := ComputePeriodScore(prevAgg, currAgg, actualPeople, periodMonths)
			delta.PeriodLeverage = ps.PeriodLeverage
			delta.IsNegative = ps.IsNegative
		} else {
			delta.IsNegative = delta.DeltaCode < 0
		}

		deltas = append(deltas, delta)
	}

	return deltas
}

// monthBucket groups weekly deltas within a single calendar month.
type monthBucket struct {
	key   string // "YYYY-MM"
	first HistoryPoint
	last  HistoryPoint
}

// bucketByMonth groups weekly delta points into calendar month buckets,
// preserving insertion order.
func bucketByMonth(weeklyDeltas []HistoryPoint) []monthBucket {
	var buckets []monthBucket
	bucketIdx := make(map[string]int)

	for _, d := range weeklyDeltas {
		key := d.Date.Format("2006-01")
		if idx, ok := bucketIdx[key]; ok {
			buckets[idx].last = d
		} else {
			bucketIdx[key] = len(buckets)
			buckets = append(buckets, monthBucket{key: key, first: d, last: d})
		}
	}
	return buckets
}

// aggregateMonthly groups weekly deltas by calendar month. For each month,
// leverage is computed from the first snapshot to the last.
func aggregateMonthly(weeklyDeltas []HistoryPoint, actualPeople int) []HistoryPoint {
	buckets := bucketByMonth(weeklyDeltas)

	monthly := make([]HistoryPoint, 0, len(buckets))
	for i, b := range buckets {
		point := HistoryPoint{
			Date:      b.last.Date,
			Projects:  b.last.Projects,
			Aggregate: b.last.Aggregate,
			TotalCode: b.last.TotalCode,
			TotalCost: b.last.TotalCost,
			Period:    PeriodMonthly,
		}

		if i == 0 && b.first.IsFirst {
			point.IsFirst = true
			point.DeltaCode = b.last.TotalCode
			point.DeltaEstCost = b.last.TotalCost
			monthly = append(monthly, point)
			continue
		}

		point.DeltaCode = b.last.TotalCode - b.first.TotalCode
		point.DeltaEstCost = b.last.TotalCost - b.first.TotalCost
		point.IsNegative = point.DeltaCode < 0

		firstAgg := snapshotAggregate(b.first)
		lastAgg := snapshotAggregate(b.last)
		if firstAgg != nil && lastAgg != nil {
			periodMonths := ElapsedMonths(b.first.Date, b.last.Date)
			ps := ComputePeriodScore(firstAgg, lastAgg, actualPeople, periodMonths)
			point.PeriodLeverage = ps.PeriodLeverage
			point.IsNegative = ps.IsNegative
		}

		monthly = append(monthly, point)
	}

	return monthly
}

// RecentLeverage computes period leverage by comparing current scores against
// the nearest snapshot on or before the cutoff date. Returns (leverage, true)
// if at least one project had snapshot data, or (0, false) if no snapshots exist.
func RecentLeverage(ctx context.Context, store SnapshotStorer, currentScores []*LeverageScore, actualPeople int, since time.Time) (float64, bool) {
	if ctx.Err() != nil {
		return 0, false
	}

	now := time.Now()
	periodMonths := ElapsedMonths(since, now)
	if periodMonths <= 0 || actualPeople <= 0 {
		return 0, false
	}

	var totalCurrentPM, totalPriorPM float64
	found := false

	for _, score := range currentScores {
		if ctx.Err() != nil {
			return 0, false
		}

		currentPM := score.EstimatedPeople * score.EstimatedMonths
		totalCurrentPM += currentPM

		snapshots, err := store.LoadAll(ctx, score.ProjectName)
		if err != nil || len(snapshots) == 0 {
			continue
		}

		sort.Slice(snapshots, func(i, j int) bool {
			return snapshots[i].Date < snapshots[j].Date
		})

		snap := findMostRecent(snapshots, since)
		if snap == nil || snap.Leverage == nil {
			continue
		}

		priorPM := snap.Leverage.EstimatedPeople * snap.Leverage.EstimatedMonths
		totalPriorPM += priorPM
		found = true
	}

	if !found {
		return 0, false
	}

	deltaPM := totalCurrentPM - totalPriorPM
	actualPM := float64(actualPeople) * periodMonths

	return deltaPM / actualPM, true
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
