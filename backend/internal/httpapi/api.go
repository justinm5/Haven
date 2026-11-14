package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"haven.local/backend/internal/intelligence"
	"haven.local/backend/internal/rentals"
	"haven.local/backend/internal/store"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type API struct {
	Store          store.Repository
	ImportToken    string
	AllowedSources map[string]bool
	Origin         string
	Live           *rentals.Client
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Store.Ready(r.Context()); err != nil {
			if a.Live.Configured() {
				write(w, 200, map[string]any{"status": "search_ready", "history_available": false})
				return
			}
			write(w, 503, map[string]string{"error": "database unavailable"})
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"name": "Haven", "live_search_available": a.Live.Configured(), "signals_are": "observations requiring review, not fraud determinations"})
	})
	mux.HandleFunc("POST /api/v1/rentals/search", a.searchRentals)
	mux.HandleFunc("GET /api/v1/properties", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if value := r.URL.Query().Get("limit"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100 {
				write(w, 400, map[string]string{"error": "limit must be 1–100"})
				return
			}
			limit = n
		}
		p, err := a.Store.List(r.Context(), limit)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, map[string]any{"properties": p, "limit": limit})
	})
	mux.HandleFunc("GET /api/v1/properties/{id}", func(w http.ResponseWriter, r *http.Request) {
		report, err := a.Store.Report(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			write(w, 404, map[string]string{"error": "property not found"})
			return
		}
		if errors.Is(err, store.ErrPending) {
			w.Header().Set("Retry-After", "2")
			write(w, 202, map[string]string{"status": "analysis_pending"})
			return
		}
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, report)
	})
	mux.HandleFunc("GET /api/v1/matches", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		addr := intelligence.Address{Street: q.Get("street"), Unit: q.Get("unit"), City: q.Get("city"), State: q.Get("state"), PostalCode: q.Get("postal_code")}
		if strings.TrimSpace(addr.Street) == "" {
			write(w, 400, map[string]string{"error": "structured street, city, state and postal_code required"})
			return
		}
		p, err := a.Store.Candidates(r.Context(), addr)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, map[string]any{"candidates": p})
	})
	mux.HandleFunc("POST /api/v1/observations", a.ingest)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Vary", "Origin")
			if origin != a.Origin {
				write(w, 403, map[string]string{"error": "origin not allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *API) searchRentals(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var q rentals.Query
	if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF {
		write(w, 400, map[string]string{"code": "invalid_search", "error": "Enter a valid area and search preferences."})
		return
	}
	if _, err := q.Parameters(); err != nil {
		write(w, 422, map[string]string{"code": "invalid_search", "error": err.Error()})
		return
	}
	if !a.Live.Configured() {
		write(w, 503, map[string]string{"code": "not_connected", "error": "Live rentals aren't available yet. Please check back soon."})
		return
	}
	result, err := a.Live.Search(r.Context(), q)
	if errors.Is(err, rentals.ErrQuota) {
		w.Header().Set("Retry-After", "3600")
		write(w, 429, map[string]string{"code": "capacity", "error": "Search is temporarily at capacity. Please try again later."})
		return
	}
	if errors.Is(err, rentals.ErrProviderAuth) {
		write(w, 503, map[string]string{"code": "provider_auth", "error": "Live rental search is temporarily unavailable while the data connection is checked."})
		return
	}
	if errors.Is(err, rentals.ErrProviderPlan) {
		write(w, 503, map[string]string{"code": "provider_plan", "error": "Live rental search is temporarily unavailable for this data source."})
		return
	}
	if errors.Is(err, rentals.ErrProviderQuota) {
		w.Header().Set("Retry-After", "3600")
		write(w, 429, map[string]string{"code": "provider_capacity", "error": "The rental data source is temporarily at its request limit. Please try again later."})
		return
	}
	if errors.Is(err, rentals.ErrProviderQuery) {
		write(w, 422, map[string]string{"code": "provider_query", "error": "The rental data source could not match that search. Try a ZIP code or city and state."})
		return
	}
	if err != nil {
		write(w, 502, map[string]string{"code": "unavailable", "error": "We couldn't reach live rentals. Please try again shortly."})
		return
	}
	write(w, 200, result)
}
func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	expected := sha256.Sum256([]byte(a.ImportToken))
	got := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	if len(a.ImportToken) < 32 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(expected[:], got[:]) != 1 {
		write(w, 401, map[string]string{"error": "authorized importer token required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var o intelligence.Observation
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&o); err != nil {
		write(w, 400, map[string]string{"error": "invalid observation JSON"})
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		write(w, 400, map[string]string{"error": "one JSON observation required"})
		return
	}
	if !a.AllowedSources[o.Source] {
		write(w, 403, map[string]string{"error": "source is not enabled for this importer"})
		return
	}
	if err := o.Validate(time.Now()); err != nil {
		write(w, 422, map[string]string{"error": err.Error()})
		return
	}
	s, err := a.Store.Insert(r.Context(), o)
	if errors.Is(err, store.ErrConflict) {
		write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 202, map[string]any{"snapshot": s, "status": "accepted", "analysis": "eventual; inspect the property report"})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	write(w, 500, map[string]string{"error": "internal service error"})
}
