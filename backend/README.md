# Haven server setup

## Live search

RentCast is the first integrated listing provider. Its [rental endpoint](https://developers.rentcast.io/reference/rental-listings-long-term) supports city/state, ZIP and geographic-radius searches, with rent and bedroom filters. Its [coverage page](https://www.rentcast.io/api) describes U.S. rental coverage. This is a practical first provider, not a measured claim that it has the best coverage in every market.

From the repository root, copy `backend/.env.example` to `backend/.env`. Get your own RentCast account/key and put it in `HAVEN_RENTCAST_API_KEY`. Never put this private key in frontend variables or commit it. Review your provider plan's billing and usage controls before enabling traffic; no subscription, account or paid request has been initiated here.

```bash
# Terminal one, from backend/ (Go 1.26+):
go run ./cmd/haven
# Terminal two, from the repository root:
npm run dev:haven
```

The service reads `.env` from its working directory. `-env-file` can select another file. It does not evaluate shell commands. Explicit process environment overrides the file. Restart after changing server configuration.

Live search can run without a database. In this configuration, property-history writes are disabled and the old property-record endpoints return empty/not-found states. No in-memory rental data is loaded. Without a RentCast key, live search explicitly returns HTTP 503 and no listings.

The frontend uses `http://127.0.0.1:8081` by default. `VITE_HAVEN_API_URL` changes that public URL; `HAVEN_WEB_ORIGIN` selects the exact permitted frontend origin, defaulting to `http://127.0.0.1:5173`.

## Search behavior

`POST /api/v1/rentals/search` accepts an entered `area` (city plus two-letter state, or ZIP), optional `max_rent_cents` and `min_beds`. Nearby requests use `latitude` and `longitude` instead of an entered area. Both client and server round coordinates to two decimals before the provider request. No separate geocoder receives them. Request bodies should not be logged by future infrastructure.

Only provider-reported active records with a valid ID, address and positive listed rent are returned. The adapter forwards filters, applies the budget/bedroom constraints again locally, deduplicates IDs and sorts the returned batch by rent. A studio preference means exactly zero bedrooms; a positive preference means that minimum number. Blank preferences do not become default budgets or bedrooms.

Each result has the source name, last-seen date, source-reported rental history when present and plain reasons based on entered filters. The source does not supply listing photographs or descriptions in this integration, and the app does not manufacture them. An agent website, when supplied as a valid HTTPS URL, is labeled as the agent's website rather than a direct listing URL.

Identical requests share a bounded ten-minute cache, with their original retrieval time. Concurrent identical requests collapse to one provider call. Paid/provider calls are not automatically retried. A default forty-call calendar-month guard applies to each running process; it resets on restart and is **not a durable billing cap**. Multiple replicas require shared quota control before public deployment. Configure hard limits/endpoint restrictions in your provider account as appropriate.

Missing credentials, upstream failures, malformed responses and empty result sets never trigger static or generated listings. The server does not perform background searches on startup. Only a user search, or a returning user's remembered search, can request provider data.

## Optional persistent history and worker stack

PostgreSQL history ingestion and entity resolution remain isolated from the existing Supabase app. For these features, use the separate development stack:

```bash
# Repository root, with Docker running:
cp backend/.env.example backend/.env
# Fill HAVEN_DB_PASSWORD and HAVEN_IMPORT_TOKEN with independent random values.
# Add HAVEN_RENTCAST_API_KEY if live search should be enabled too.
docker compose --env-file backend/.env -f compose.haven.yml up --build
```

All host ports are loopback-bound: API 8081, PostgreSQL 5433 and Redis 6380. The separate `haven-intelligence` project owns its volumes. Its migration service creates the `haven` schema and does not seed listings. `down` stops it without deleting volumes; adding `--volumes` would erase its stored data.

`POST /api/v1/observations` is still the protected importer endpoint. It requires a token of at least 32 characters, a configured source allowlist and a real database. Real observation inputs require source URL, permission reference, structured Amherst pilot address, past capture time, positive monthly whole-unit rent cents, known/unknown availability and optional valid image digests. A permission-reference string is a trusted importer's attestation, not independent proof of collection rights.

The operator command `go run ./cmd/haven -mode import -file <your-observations.json>` requires an explicit input file, source allowlist and database. There is no default input file. Replays are idempotent, and conflicting content under the same source/listing/time returns 409. The API accepts observations for eventual analysis; acceptance is not independent verification.

## Endpoints

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Process liveness |
| `GET /readyz` | Database ready, or live-search configured; 503 if neither is available |
| `GET /api/v1/meta` | Whether a live provider is configured |
| `POST /api/v1/rentals/search` | Real provider search with customer preferences |
| `GET /api/v1/properties?limit=100` | Imported canonical properties, if any |
| `GET /api/v1/properties/{id}` | Imported history/evidence; 202 until first analysis |
| `GET /api/v1/matches` | Review-only address match candidates |
| `POST /api/v1/observations` | Authenticated, allowlisted history ingestion |

## Verification

```bash
# backend/
go test -race ./...
go vet ./...
go build ./cmd/haven
# repository root
npm test
npm run typecheck:haven
npm run build
npm run build:haven
```

Provider contract/error/cache tests use a local HTTP test server inside `_test.go` files. Those tests never become app data and are excluded from the backend container. No live credentialed provider call has been verified because no key has been supplied. PostgreSQL/Redis integration remains configured in CI but unverified locally under this environment's restrictions.
