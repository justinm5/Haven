# Haven

A simple rental search tailored to the customer's area, budget and bedrooms, with source-reported rental history and clear cost calculations.

## Start the real-data connection

1. Get an API key from your [RentCast account](https://app.rentcast.io/). Review your plan and configure its usage controls before enabling customer traffic.
2. Copy `backend/.env.example` to `backend/.env`. Set `HAVEN_RENTCAST_API_KEY` there. This is a private server key, not a VITE variable.
3. Start the Go service from `backend/` with `go run ./cmd/haven` (Go 1.26+). It reads that directory's `.env` without executing shell code. Existing environment variables take precedence.
4. From the repository root, run `npm ci --ignore-scripts` and `npm run dev:haven`.
5. Open `http://127.0.0.1:5173/haven.html`.

A provider account/key is required for live results. None has been configured in this workspace. Without one, customers see a clear unavailable state and no listings. There is no demo mode, seeded rental dataset or static listing fallback.

## Customer experience

- One search form: area, optional maximum rent and optional bedrooms.
- Returning customers' entered preferences are remembered on their browser and searched automatically.
- Customers can choose device location when live search is available. It is never requested automatically. Coordinates are rounded before being sent to RentCast for a ten-mile nearby search; they are not saved to browser storage.
- Results show why they fit, the provider's last-seen date, and listed rent. The first 50 source matches are sorted by listed rent; this is not a claim to rank every rental on the market.
- Saved rentals remain on that browser. History and full-cost planning open only when the customer asks for details.
- Empty results and provider failures never generate replacement listings. Photos and descriptions are not supplied by this integration, so none are invented.

## Existing functionality

The existing React/Supabase application retains its routes, authentication, documents, saved/applied listing flows, search and standard build. Its hardcoded landing-page rental preview and unused prototype were removed; the listing seed migration now makes no data changes. No database records were deleted or migrations applied.

The separate Haven build does not require Supabase. Go/PostgreSQL history ingestion, entity matching and worker processing remain available for permitted observations. They are separate from the new nationwide live search and are not required to search RentCast.

```bash
npm test
npm run typecheck:haven
npm run build
npm run build:haven
```

See [backend setup](backend/README.md) for the API, data source and optional database setup.
