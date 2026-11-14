// Package vercelhandler adapts the Haven API for Vercel's Go function runtime.
package vercelhandler

import (
	"haven.local/backend/internal/httpapi"
	"haven.local/backend/internal/rentals"
	"haven.local/backend/internal/store"
	"net/http"
	"os"
	"strings"
)

const productionOrigin = "https://haven-two-ruby.vercel.app"

// Handler builds the same API used by the standalone Go server. Rental search
// remains live-provider-only; persistent history is unavailable in this
// serverless adapter until a database is configured.
func Handler() http.Handler {
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("HAVEN_WEB_ORIGIN")), "/")
	if origin == "" {
		origin = productionOrigin
	}
	allowed := map[string]bool{}
	for _, source := range strings.Split(os.Getenv("HAVEN_ALLOWED_SOURCES"), ",") {
		if source = strings.TrimSpace(source); source != "" {
			allowed[source] = true
		}
	}
	return (&httpapi.API{
		Store:          store.Unavailable{},
		ImportToken:    os.Getenv("HAVEN_IMPORT_TOKEN"),
		AllowedSources: allowed,
		Origin:         origin,
		Live:           rentals.New(os.Getenv("HAVEN_RENTCAST_API_KEY")),
	}).Handler()
}
