package store

import (
	"context"
	"errors"
	"haven.local/backend/internal/intelligence"
	"sync"
	"testing"
	"time"
)

func sample() intelligence.Observation {
	return intelligence.Observation{Source: "test", SourceListingID: "1", SourceURL: "https://example.com/1", PermissionReference: "synthetic", Address: intelligence.Address{Street: "12 Main Street", Unit: "2", City: "Amherst", State: "MA", PostalCode: "01002"}, ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RentCents: 200000, PricingBasis: "whole_unit_month", Availability: "available"}
}
func TestConcurrentReplayAndConflictingIdentity(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	o := sample()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Insert(ctx, o); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s, _ := m.Insert(ctx, o)
	report, err := m.Report(ctx, s.PropertyID)
	if err != nil || len(report.Timeline) != 1 {
		t.Fatalf("replay duplicated history: %v %+v", err, report)
	}
	o.RentCents = 1
	if _, err = m.Insert(ctx, o); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting observation changed history: %v", err)
	}
}
func TestCandidateRequiresReview(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	o := sample()
	_, _ = m.Insert(ctx, o)
	addr := o.Address
	addr.Street = "12 Maine Street"
	cs, err := m.Candidates(ctx, addr)
	if err != nil || len(cs) != 1 || !cs[0].RequiresReview {
		t.Fatalf("bad candidate: %v %+v", err, cs)
	}
}
func TestReportCarriesCrossPropertyImageEvidence(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	a := sample()
	a.ImageHashes = []string{intelligence.Digest("image")}
	b := a
	b.Address.Street = "88 Example Road"
	b.SourceListingID = "2"
	s, _ := m.Insert(ctx, a)
	_, _ = m.Insert(ctx, b)
	r, err := m.Report(ctx, s.PropertyID)
	if err != nil || len(r.Signals) != 1 || len(r.Evidence) != 2 {
		t.Fatalf("evidence missing: %v %+v", err, r)
	}
}
