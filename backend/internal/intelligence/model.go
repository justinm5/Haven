package intelligence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Address struct {
	Street     string `json:"street"`
	Unit       string `json:"unit"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
}

type Observation struct {
	Source              string    `json:"source"`
	SourceListingID     string    `json:"source_listing_id"`
	SourceURL           string    `json:"source_url"`
	PermissionReference string    `json:"permission_reference"`
	Address             Address   `json:"address"`
	ObservedAt          time.Time `json:"observed_at"`
	RentCents           int64     `json:"rent_cents"`
	PricingBasis        string    `json:"pricing_basis"`
	Beds                *int      `json:"beds,omitempty"`
	Baths               *float64  `json:"baths,omitempty"`
	Availability        string    `json:"availability"`
	Description         string    `json:"description"`
	ImageHashes         []string  `json:"image_hashes"`
}

type Snapshot struct {
	ID         string `json:"id"`
	PropertyID string `json:"property_id"`
	Observation
}
type Property struct {
	ID           string  `json:"id"`
	Address      Address `json:"address"`
	CanonicalKey string  `json:"canonical_key"`
}
type Signal struct {
	ID          string   `json:"id"`
	PropertyID  string   `json:"property_id"`
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	SnapshotIDs []string `json:"snapshot_ids"`
	Explanation string   `json:"explanation"`
}
type Report struct {
	AnalyzedAt        time.Time  `json:"analyzed_at"`
	ProcessingPending bool       `json:"processing_pending"`
	Property          Property   `json:"property"`
	Timeline          []Snapshot `json:"timeline"`
	Signals           []Signal   `json:"signals"`
	Evidence          []Snapshot `json:"evidence"`
}
type Candidate struct {
	Property       Property `json:"property"`
	Similarity     float64  `json:"similarity"`
	RequiresReview bool     `json:"requires_review"`
	Reason         string   `json:"reason"`
}

var unitMarker = regexp.MustCompile(`(?i)\s*(?:#\s*|\bapt\.?\s*|\bapartment\s+|\bunit\s+)([a-z0-9-]+)\s*$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func tokens(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '-' {
			return r
		}
		return ' '
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

// Normalize uses structured address fields. It intentionally does not infer a
// missing unit, city or postal code, or conflate Main Street with Main Road.
func Normalize(a Address) Address {
	if match := unitMarker.FindStringSubmatchIndex(a.Street); match != nil {
		if strings.TrimSpace(a.Unit) == "" {
			a.Unit = a.Street[match[2]:match[3]]
		}
		a.Street = a.Street[:match[0]]
	}
	a.Street = tokens(a.Street)
	parts := strings.Fields(a.Street)
	suffixes := map[string]string{"street": "st", "avenue": "ave", "road": "rd", "drive": "dr", "lane": "ln", "boulevard": "blvd", "court": "ct", "place": "pl", "north": "n", "south": "s", "east": "e", "west": "w"}
	for i, p := range parts {
		if replacement, ok := suffixes[p]; ok {
			parts[i] = replacement
		}
	}
	a.Street = strings.Join(parts, " ")
	a.Unit = tokens(a.Unit)
	a.Unit = strings.TrimSpace(regexp.MustCompile(`^(apt|apartment|unit)\s+`).ReplaceAllString(a.Unit, ""))
	a.Unit = strings.TrimPrefix(a.Unit, "#")
	a.City = tokens(a.City)
	a.State = strings.ToUpper(strings.TrimSpace(a.State))
	if strings.EqualFold(a.State, "massachusetts") {
		a.State = "MA"
	}
	a.PostalCode = strings.TrimSpace(a.PostalCode)
	if len(a.PostalCode) > 5 && a.PostalCode[5] == '-' {
		a.PostalCode = a.PostalCode[:5]
	}
	return a
}
func Key(a Address) string {
	a = Normalize(a)
	return strings.Join([]string{a.Street, a.Unit, a.City, a.State, a.PostalCode}, "|")
}
func Digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
func PropertyID(a Address) string { return Digest(Key(a))[:24] }
func ObservationID(o Observation) string {
	return Digest(o.Source, o.SourceListingID, o.ObservedAt.UTC().Format(time.RFC3339Nano))
}

func (o Observation) Validate(now time.Time) error {
	if match := unitMarker.FindStringSubmatch(o.Address.Street); match != nil && o.Address.Unit != "" {
		if Normalize(Address{Unit: match[1]}).Unit != Normalize(Address{Unit: o.Address.Unit}).Unit {
			return fmt.Errorf("street unit and structured unit conflict")
		}
	}
	a := Normalize(o.Address)
	if !sourcePattern.MatchString(o.Source) || strings.TrimSpace(o.SourceListingID) == "" || len(o.SourceListingID) > 200 {
		return fmt.Errorf("valid source and source_listing_id required")
	}
	u, err := url.Parse(o.SourceURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("source_url must be an HTTP(S) URL without credentials")
	}
	if strings.TrimSpace(o.PermissionReference) == "" || len(o.PermissionReference) > 1000 {
		return fmt.Errorf("permission_reference required for provenance")
	}
	if len(a.Street) < 3 || a.City != "amherst" || a.State != "MA" || !regexp.MustCompile(`^0100[234]$`).MatchString(a.PostalCode) {
		return fmt.Errorf("pilot requires an Amherst, MA address with ZIP 01002, 01003 or 01004")
	}
	if !unicode.IsDigit(rune(a.Street[0])) {
		return fmt.Errorf("street number required")
	}
	if strings.ContainsAny(Key(a), "|\x00") && strings.Count(Key(a), "|") != 4 {
		return fmt.Errorf("invalid address delimiter")
	}
	if o.ObservedAt.IsZero() || o.ObservedAt.After(now.Add(5*time.Minute)) || o.ObservedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return fmt.Errorf("observed_at must be a valid past observation")
	}
	if o.RentCents <= 0 || o.RentCents > 100_000_000 {
		return fmt.Errorf("rent_cents must be positive and at most 100000000")
	}
	if o.PricingBasis != "whole_unit_month" {
		return fmt.Errorf("pilot imports require pricing_basis whole_unit_month; per-room and unknown prices are not comparable")
	}
	if o.Beds != nil && (*o.Beds < 0 || *o.Beds > 30) {
		return fmt.Errorf("beds out of range")
	}
	if o.Baths != nil && (*o.Baths < 0 || *o.Baths > 30) {
		return fmt.Errorf("baths out of range")
	}
	if o.Availability != "available" && o.Availability != "unavailable" && o.Availability != "unknown" {
		return fmt.Errorf("availability must be available, unavailable or unknown")
	}
	if len(o.Description) > 20000 || len(o.ImageHashes) > 30 {
		return fmt.Errorf("description or image list too large")
	}
	for _, h := range o.ImageHashes {
		if !hashPattern.MatchString(h) {
			return fmt.Errorf("image_hashes must contain lowercase SHA-256 content hashes")
		}
	}
	return nil
}

// Similarity suggests candidates only. A different or missing unit never gets
// auto-merged, even when a building's street address is identical.
func Similarity(a, b Address) float64 {
	a, b = Normalize(a), Normalize(b)
	if a.City != b.City || a.State != b.State || a.PostalCode != b.PostalCode || a.Unit != b.Unit {
		return 0
	}
	pa, pb := strings.Fields(a.Street), strings.Fields(b.Street)
	if len(pa) == 0 || len(pb) == 0 || pa[0] != pb[0] {
		return 0
	}
	grams := func(s string) map[string]bool {
		m := map[string]bool{}
		r := []rune("  " + s + "  ")
		for i := 0; i+2 < len(r); i++ {
			m[string(r[i:i+3])] = true
		}
		return m
	}
	x, y := grams(a.Street), grams(b.Street)
	n := 0
	for k := range x {
		if y[k] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(x)+len(y))
}

// Analyze is deterministic over event time, so late observations can be safely
// replayed. IDs make its results idempotent. Findings are observations, not fraud verdicts.
func Analyze(propertyID string, history, others []Snapshot) []Signal {
	history = append([]Snapshot(nil), history...)
	sort.Slice(history, func(i, j int) bool {
		if history[i].ObservedAt.Equal(history[j].ObservedAt) {
			return history[i].ID < history[j].ID
		}
		return history[i].ObservedAt.Before(history[j].ObservedAt)
	})
	result := []Signal{}
	add := func(kind, summary, explanation string, ids ...string) {
		sort.Strings(ids)
		result = append(result, Signal{ID: Digest(append([]string{kind}, ids...)...), PropertyID: propertyID, Kind: kind, Summary: summary, Explanation: explanation, SnapshotIDs: ids})
	}
	previous := map[string]Snapshot{}
	for _, s := range history {
		key := s.Source + "\x00" + s.SourceListingID
		if p, ok := previous[key]; ok {
			if p.RentCents != s.RentCents {
				add("rent_changed", fmt.Sprintf("Listed rent changed from $%.2f to $%.2f", float64(p.RentCents)/100, float64(s.RentCents)/100), "Consecutive observations of the same source listing. Fees, concessions and lease terms may explain a difference.", p.ID, s.ID)
				delta := float64(s.RentCents-p.RentCents) / float64(p.RentCents)
				if delta <= -0.25 || delta >= 0.25 {
					add("large_price_change", "Listed rent changed by at least 25%", "Rule threshold, not a market valuation or fraud prediction. Confirm unit, lease term and concessions with the source.", p.ID, s.ID)
				}
			}
			if p.Availability != s.Availability {
				add("availability_changed", "Availability status changed", "Source-reported availability may lag actual availability.", p.ID, s.ID)
			}
			if strings.TrimSpace(p.Description) != strings.TrimSpace(s.Description) {
				add("description_changed", "Listing description changed", "Compare the two stored descriptions; edits alone do not establish misleading conduct.", p.ID, s.ID)
			}
		}
		previous[key] = s
	}
	// Compare each source's latest observation. Ignore older than 48 hours
	// relative to the newest observation to avoid stale cross-source conflicts.
	latest := []Snapshot{}
	for _, s := range previous {
		latest = append(latest, s)
	}
	for i, a := range latest {
		for _, b := range latest[i+1:] {
			if a.Source == b.Source {
				continue
			}
			age := a.ObservedAt.Sub(b.ObservedAt)
			if age < 0 {
				age = -age
			}
			if age > 48*time.Hour {
				continue
			}
			conflict := a.RentCents != b.RentCents || (a.Beds != nil && b.Beds != nil && *a.Beds != *b.Beds) || (a.Baths != nil && b.Baths != nil && *a.Baths != *b.Baths) || (a.Availability != "unknown" && b.Availability != "unknown" && a.Availability != b.Availability)
			if conflict {
				add("cross_source_conflict", "Recent sources report different listing details", "Latest observations within 48 hours differ in rent, beds, baths or known availability. Confirm the lease terms and source freshness.", a.ID, b.ID)
			}
		}
	}
	imageEvidence := map[string]map[string]bool{}
	for _, a := range history {
		for _, b := range others {
			if a.PropertyID == b.PropertyID {
				continue
			}
			found := false
			for _, x := range a.ImageHashes {
				for _, y := range b.ImageHashes {
					if x == y {
						found = true
					}
				}
			}
			if found {
				if imageEvidence[b.PropertyID] == nil {
					imageEvidence[b.PropertyID] = map[string]bool{}
				}
				imageEvidence[b.PropertyID][a.ID] = true
				imageEvidence[b.PropertyID][b.ID] = true
			}
		}
	}
	for _, evidence := range imageEvidence {
		ids := []string{}
		for id := range evidence {
			ids = append(ids, id)
		}
		add("image_reused", "Identical image bytes were reported for different addresses", "Exact SHA-256 content match supplied by the permitted importer. Shared building photos and syndicated images can be legitimate. Cropped or edited images are not detected.", ids...)
	}

	dedup := map[string]Signal{}
	for _, s := range result {
		dedup[s.ID] = s
	}
	result = result[:0]
	for _, s := range dedup {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
