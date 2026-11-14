package httpapi

import (
	"bytes"
	"encoding/json"
	"haven.local/backend/internal/intelligence"
	"haven.local/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportContract(t *testing.T) {
	token := strings.Repeat("x", 32)
	api := (&API{Store: store.NewMemory(), ImportToken: token, AllowedSources: map[string]bool{"test": true}, Origin: "http://localhost:5173"}).Handler()
	o := intelligence.Observation{Source: "test", SourceListingID: "one", SourceURL: "https://example.com/one", PermissionReference: "synthetic", Address: intelligence.Address{Street: "12 Main Street", Unit: "2", City: "Amherst", State: "MA", PostalCode: "01002"}, ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RentCents: 200000, PricingBasis: "whole_unit_month", Availability: "available"}
	payload, _ := json.Marshal(o)
	request := func(body []byte, key string, code int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/observations", bytes.NewReader(body))
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("want %d got %d: %s", code, w.Code, w.Body)
		}
	}
	request(payload, "", 401)
	request(payload, "wrong", 401)
	request(payload, token, 202)
	request(payload, token, 202)
	changed := o
	changed.RentCents = 1
	body, _ := json.Marshal(changed)
	request(body, token, 409)
	changed = o
	changed.Source = "disabled"
	body, _ = json.Marshal(changed)
	request(body, token, 403)
	request(append(payload, []byte(` {}`)...), token, 400)
	request([]byte(`{"unknown_field":true}`), token, 400)
	changed = o
	changed.PermissionReference = ""
	body, _ = json.Marshal(changed)
	request(body, token, 422)
}
func TestReadEndpointsAndCORS(t *testing.T) {
	api := (&API{Store: store.NewMemory(), Origin: "http://localhost:5173"}).Handler()
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/api/v1/meta": 200, "/api/v1/properties": 200, "/api/v1/properties/no-such-property": 404, "/api/v1/properties?limit=100000": 400, "/api/v1/matches": 400} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: got %d want %d", path, w.Code, want)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/properties", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
}

func TestLiveSearchWithoutProviderIsExplicitlyUnavailable(t *testing.T) {
	api := (&API{Store: store.Unavailable{}}).Handler()
	r := httptest.NewRequest("POST", "/api/v1/rentals/search", strings.NewReader(`{"area":"01002"}`))
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "not_connected") {
		t.Fatalf("unexpected fallback: %d %s", w.Code, w.Body)
	}
	r = httptest.NewRequest("GET", "/api/v1/properties", nil)
	w = httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"properties":[]`) {
		t.Fatalf("unconfigured history contains unexpected data: %s", w.Body)
	}
}
