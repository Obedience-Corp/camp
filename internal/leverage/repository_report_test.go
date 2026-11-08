package leverage

import (
	"context"
	"testing"
	"time"
)

func TestScopedScanReconcilesManifest(t *testing.T) {
	valid := []byte(`{"languageSummary":[{"Name":"Go","Count":1,"Lines":3,"Code":2,"Comment":0,"Blank":1,"Files":[{"Location":"a.go","Lines":3,"Code":2,"Comment":0,"Blank":1}]}],"estimatedPeople":2,"estimatedScheduleMonths":3,"estimatedCost":100}`)
	scan, err := decodeScopedScan("/repo", valid)
	if err != nil || len(scan.Files) != 1 {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
	for _, data := range []string{
		`{"languageSummary":[{"Count":1}]}`,
		`{"languageSummary":[{"Count":1,"Lines":3,"Files":[{"Location":"a.go","Lines":4}]}]}`,
		`{"languageSummary":[{"Count":1,"Files":[{"Location":"../escape"}]}]}`,
		`{"estimatedPeople":-1}`,
	} {
		if _, err := decodeScopedScan("/repo", []byte(data)); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
}

func TestManifestBlameUncommittedAndTruncated(t *testing.T) {
	input := "0000000000000000000000000000000000000000 1 1 1\nauthor Fake Person\nauthor-mail <fake@x>\n\tnew\n" +
		"1111111111111111111111111111111111111111 2 2 1\nauthor Alice\nauthor-mail <alice@x>\n\tkept\n"
	result, err := parseManifestBlame(input, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.buckets["uncommitted"] != 1 || result.owners["alice@x"].lines != 1 || len(result.owners) != 1 {
		t.Fatalf("bad attribution: %+v", result)
	}
	if _, err := parseManifestBlame(input, 3); err == nil {
		t.Fatal("accepted count mismatch")
	}
	if _, err := parseManifestBlame("1111111111111111111111111111111111111111 1 1\nauthor Alice\n", 1); err == nil {
		t.Fatal("accepted truncated blame")
	}
}

func TestContributorThresholdDoesNotChangeAllocation(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	last := first.Add(time.Duration(2*30.44*24*60*60) * time.Second)
	for _, threshold := range []float64{0, 15} {
		r := newRepositoryReport()
		r.Summary.Lines = 1000
		r.Summary.EstimatedPM = 10
		r.Summary.Buckets["contributors"] = &AccountingBucket{Lines: 900, EstimatedPM: 9}
		r.Summary.Buckets["uncommitted"] = &AccountingBucket{Lines: 100, EstimatedPM: 1}
		r.finish(map[string]*Contributor{
			"alice": {ID: "alice", Lines: 800, EstimatedPM: 8, First: &first, Last: &last, HistoryComplete: true},
			"bob":   {ID: "bob", Lines: 100, EstimatedPM: 1, First: &first, Last: &first, HistoryComplete: true},
		}, threshold)
		if len(r.all) != 2 || r.Summary.Buckets["contributors"].EstimatedPM != 9 {
			t.Fatal("threshold changed raw allocation")
		}
		if threshold == 15 && (len(r.Contributors) != 1 || r.Contributors[0].EstimatedPM != 8 || r.Display.Omitted != 1) {
			t.Fatalf("bad filtering: %+v", r)
		}
		if threshold == 0 && (r.Contributors[0].ID != "bob" || *r.Contributors[0].Leverage != 10) {
			t.Fatalf("bad floor/sort: %+v", r.Contributors)
		}
	}
}

func TestMissingHistoryIsNotSingleCommit(t *testing.T) {
	r := newRepositoryReport()
	r.Summary.Lines = 10
	r.finish(map[string]*Contributor{"unknown": {ID: "unknown", Lines: 10, EstimatedPM: 5}}, 0)
	if r.Contributors[0].Leverage != nil || r.Contributors[0].ElapsedPM != nil {
		t.Fatal("invented effort for unknown history")
	}
}

func TestManifestBlameCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := collectManifestBlame(ctx, "/unused", "/unused", nil, nil)
	if err != context.Canceled {
		t.Fatalf("error=%v", err)
	}
}

func TestPersonalProjectionAgreesWithPartialContributor(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := newRepositoryReport()
	r.Summary.Lines = 100
	r.Status = "partial"
	r.Summary.Buckets["unavailable"].Lines = 10
	r.Projects = []ReportProject{{Target: "fixture", EstimatedPM: 10, result: &SCCResult{EstimatedPeople: 2, EstimatedScheduleMonths: 5}, spans: map[string]reportSpan{"alice@x": {first: first, last: first, emails: []string{"alice@x"}}}}}
	r.finish(map[string]*Contributor{"alice@x": {ID: "alice@x", Lines: 90, EstimatedPM: 9, First: &first, Last: &first, HistoryComplete: true}}, 0)
	score, matched, _, err := r.Score("alice@x", 0, NewAuthorResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || score.FullLeverage == nil || *score.FullLeverage != *r.Contributors[0].Leverage {
		t.Fatalf("personal %+v differs from row %+v", score, r.Contributors[0])
	}
	// Partial ownership cannot support an automatic whole-repository denominator.
	summary, _, _, err := r.Score("", 0, NewAuthorResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	if summary.FullLeverage != nil {
		t.Fatal("invented complete summary effort from partial ownership")
	}
}
