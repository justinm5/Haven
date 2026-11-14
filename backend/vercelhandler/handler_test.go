package vercelhandler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetaReportsProviderStateAsJSON(t *testing.T) {
	t.Setenv("HAVEN_RENTCAST_API_KEY", "")
	t.Setenv("HAVEN_WEB_ORIGIN", productionOrigin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	req.Header.Set("Origin", productionOrigin)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content type = %q, want JSON", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("meta response is not JSON: %v", err)
	}
	if body["live_search_available"] != false {
		t.Fatalf("live_search_available = %v, want false", body["live_search_available"])
	}
}

func TestSearchWithoutProviderKeyReturnsStructuredJSON(t *testing.T) {
	t.Setenv("HAVEN_RENTCAST_API_KEY", "")
	t.Setenv("HAVEN_WEB_ORIGIN", productionOrigin)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rentals/search", strings.NewReader(`{"area":"Amherst, MA"}`))
	req.Header.Set("Origin", productionOrigin)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, req)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content type = %q, want JSON", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("search response is not JSON: %v", err)
	}
	if body["code"] != "not_connected" {
		t.Fatalf("code = %v, want not_connected", body["code"])
	}
}
