package rentals

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnconfiguredSearchNeverReturnsInventedListings(t *testing.T) {
	c := New("")
	r, err := c.Search(context.Background(), Query{Area: "01002"})
	if !errors.Is(err, ErrNotConfigured) || len(r.Listings) != 0 {
		t.Fatalf("unexpected fallback %+v %v", r, err)
	}
}
func TestSearchParametersAndApproximateLocation(t *testing.T) {
	lat, lon := 42.37322, -72.51993
	beds := 2
	p, err := (Query{Latitude: &lat, Longitude: &lon, MaxRentCents: 200000, MinBeds: &beds}).Parameters()
	if err != nil || p.Get("latitude") != "42.37" || p.Get("longitude") != "-72.52" || p.Get("price") != "1:2000.00" || p.Get("bedrooms") != "2:10" || p.Get("status") != "Active" {
		t.Fatalf("bad filters %v %v", p, err)
	}
	for _, q := range []Query{{}, {Area: "anywhere"}, {Latitude: &lat}, {Area: "01002", MaxRentCents: -1}} {
		if _, err = q.Parameters(); err == nil {
			t.Fatalf("invalid query accepted %+v", q)
		}
	}
}
func TestConcurrentSearchIsCachedAndPreservesRealHistory(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Error("missing server-side API key")
		}
		if r.URL.Query().Get("zipCode") != "01002" {
			t.Error("ZIP filter missing")
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "contract-record", "formattedAddress": "contract-test-address", "city": "Amherst", "state": "MA", "status": "Active", "price": 1800, "bedrooms": 2, "bathrooms": 1, "lastSeenDate": "2026-11-01T12:00:00Z", "history": map[string]any{"2025-01-01": map[string]any{"price": 1900, "event": "Rental Listing"}}}, {"id": "inactive", "formattedAddress": "contract-test-address", "status": "Inactive", "price": 1}})
	}))
	defer server.Close()
	c := New("test-key")
	c.endpoint = server.URL
	c.HTTP = server.Client()
	c.MaxMonthlyCalls = 1
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := c.Search(context.Background(), Query{Area: "01002"})
			if err != nil || len(r.Listings) != 1 || r.Listings[0].RentCents != 180000 || len(r.Listings[0].History) != 1 {
				t.Errorf("bad result %+v %v", r, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("provider called %d times", calls.Load())
	}
	if _, err := c.Search(context.Background(), Query{Area: "01003"}); !errors.Is(err, ErrQuota) {
		t.Fatalf("call guard bypassed: %v", err)
	}
}
func TestProviderFailureHasNoFallbackOrAutomaticRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "private upstream detail", 401)
	}))
	defer server.Close()
	c := New("test-key")
	c.endpoint = server.URL
	c.HTTP = server.Client()
	r, err := c.Search(context.Background(), Query{Area: "01002"})
	if !errors.Is(err, ErrUnavailable) || len(r.Listings) != 0 || calls.Load() != 1 {
		t.Fatalf("unexpected retry or fallback %+v %v", r, err)
	}
}
func TestEmptySourceRemainsEmpty(t *testing.T) {
	r := normalize(nil, Query{Area: "01002"}, time.Now())
	if r.Listings == nil || len(r.Listings) != 0 {
		t.Fatal("empty provider response must produce an empty JSON array")
	}
}
