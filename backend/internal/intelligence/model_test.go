package intelligence

import (
	"reflect"
	"testing"
	"time"
)

func observation() Observation {
	return Observation{Source: "permitted-feed", SourceListingID: "listing-1", SourceURL: "https://example.com/1", PermissionReference: "test fixture", Address: Address{Street: "12 Main Street", Unit: "2", City: "Amherst", State: "MA", PostalCode: "01002"}, ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RentCents: 200000, PricingBasis: "whole_unit_month", Availability: "available", Description: "Original description", ImageHashes: []string{}}
}
func snap(o Observation) Snapshot {
	return Snapshot{ID: ObservationID(o), PropertyID: PropertyID(o.Address), Observation: o}
}
func count(signals []Signal, kind string) int {
	n := 0
	for _, s := range signals {
		if s.Kind == kind {
			n++
		}
	}
	return n
}

func TestNormalizeEquivalentAddresses(t *testing.T) {
	a := observation().Address
	for index, street := range []string{"12 Main St.", " 12 MAIN STREET Apt. 2 ", "12 Main Street # 2"} {
		b := Address{Street: street, City: "AMHERST", State: "Massachusetts", PostalCode: "01002-1234"}
		if index == 0 {
			b.Unit = "Unit 2"
		}
		if Key(a) != Key(b) {
			t.Fatalf("not equivalent: %q and %q", Key(a), Key(b))
		}
	}
}
func TestUnitAndStreetSeparation(t *testing.T) {
	a := observation().Address
	for _, b := range []Address{{Street: a.Street, Unit: "3", City: a.City, State: a.State, PostalCode: a.PostalCode}, {Street: a.Street, City: a.City, State: a.State, PostalCode: a.PostalCode}, {Street: "13 Main Street", Unit: "2", City: a.City, State: a.State, PostalCode: a.PostalCode}} {
		if Key(a) == Key(b) || Similarity(a, b) != 0 {
			t.Fatalf("unsafe candidate merge: %+v", b)
		}
	}
	b := a
	b.Street = "12 Main Road"
	if Key(a) == Key(b) {
		t.Fatal("road merged into street")
	}
}
func TestSimilarityIsCandidateNotExactIdentity(t *testing.T) {
	a := observation().Address
	b := a
	b.Street = "12 Maine Street"
	if score := Similarity(a, b); score < 0.65 || score >= 1 {
		t.Fatalf("unexpected similarity %v", score)
	}
	if PropertyID(a) == PropertyID(b) {
		t.Fatal("fuzzy address was auto-merged")
	}
}
func TestValidation(t *testing.T) {
	base := observation()
	now := base.ObservedAt.Add(time.Hour)
	cases := map[string]func(*Observation){"missing provenance": func(o *Observation) { o.PermissionReference = "" }, "future": func(o *Observation) { o.ObservedAt = now.Add(time.Hour) }, "outside market": func(o *Observation) { o.Address.City = "Boston" }, "bad URL": func(o *Observation) { o.SourceURL = "javascript:alert(1)" }, "bad hash": func(o *Observation) { o.ImageHashes = []string{"url-not-hash"} }, "unit conflict": func(o *Observation) { o.Address.Street = "12 Main Street Apt 3" }, "price basis": func(o *Observation) { o.PricingBasis = "room_month" }, "negative price": func(o *Observation) { o.RentCents = -1 }}
	if err := base.Validate(now); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := base
			mutate(&o)
			if o.Validate(now) == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
func TestLateObservationsAreAnalyzedByEventTime(t *testing.T) {
	a := observation()
	b := a
	b.ObservedAt = a.ObservedAt.Add(24 * time.Hour)
	b.RentCents = 140000
	b.Description = "Updated description"
	b.Availability = "unavailable"
	sa, sb := snap(a), snap(b)
	one := Analyze(sa.PropertyID, []Snapshot{sa, sb}, nil)
	two := Analyze(sa.PropertyID, []Snapshot{sb, sa}, nil)
	if !reflect.DeepEqual(one, two) {
		t.Fatal("analysis depends on ingest order")
	}
	for _, kind := range []string{"rent_changed", "large_price_change", "description_changed", "availability_changed"} {
		if count(one, kind) != 1 {
			t.Fatalf("missing %s", kind)
		}
	}
}
func TestCrossSourceConflictIgnoresStaleSources(t *testing.T) {
	a := observation()
	b := a
	b.Source = "other-feed"
	b.RentCents = 220000
	b.ObservedAt = a.ObservedAt.Add(time.Hour)
	sa := snap(a)
	if count(Analyze(sa.PropertyID, []Snapshot{sa, snap(b)}, nil), "cross_source_conflict") != 1 {
		t.Fatal("conflict not found")
	}
	b.ObservedAt = a.ObservedAt.Add(72 * time.Hour)
	if count(Analyze(sa.PropertyID, []Snapshot{sa, snap(b)}, nil), "cross_source_conflict") != 0 {
		t.Fatal("stale source compared")
	}
}
func TestReusedImagesIncludeBothEvidenceIDs(t *testing.T) {
	a := observation()
	a.ImageHashes = []string{Digest("image")}
	b := a
	b.SourceListingID = "other-address"
	b.Address.Street = "98 Example Avenue"
	sa, sb := snap(a), snap(b)
	signals := Analyze(sa.PropertyID, []Snapshot{sa}, []Snapshot{sb})
	if len(signals) != 1 || signals[0].Kind != "image_reused" || len(signals[0].SnapshotIDs) != 2 {
		t.Fatalf("missing evidence: %+v", signals)
	}
	if len(Analyze(sa.PropertyID, []Snapshot{sa}, []Snapshot{sa})) != 0 {
		t.Fatal("same-property photos flagged")
	}
}
func TestUnchangedObservationsDoNotInventSignals(t *testing.T) {
	a := observation()
	b := a
	b.ObservedAt = a.ObservedAt.Add(time.Hour)
	if signals := Analyze(PropertyID(a.Address), []Snapshot{snap(a), snap(b)}, nil); len(signals) != 0 {
		t.Fatalf("unexpected signals %+v", signals)
	}
}
