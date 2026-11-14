# Haven 🏠

**Rental search with clearer costs and listing details.**

Haven helps renters search live listings, compare known rental costs, and review available property history. It uses real provider data; it does not include sample listings or generated scam scores.

## What it does

- Search rentals by area, budget, and bedrooms.
- See the listing source, last-seen date, and rental history when the provider supplies it.
- Save rentals in your browser and compare known monthly and move-in costs.
- Review recorded property changes and evidence when permitted observations have been collected.

## Tech stack

| Part | Technology |
| --- | --- |
| Web app | React, TypeScript, Vite |
| API and workers | Go |
| Property history | PostgreSQL |
| Cache and job queue | Redis |
| Live rental listings | RentCast API |

## Run locally

1. Install Node.js and Go 1.26 or newer.
2. Copy `backend/.env.example` to `backend/.env` and add your RentCast API key as `HAVEN_RENTCAST_API_KEY`.
3. In one terminal, run the API:

   ```bash
   cd backend
   go run ./cmd/haven
   ```

4. In another terminal, run the web app from the project root:

   ```bash
   npm ci
   npm run dev:haven
   ```

5. Open the local URL shown by Vite, then choose an area to search.

Without a RentCast key, the app still opens but live search is unavailable. PostgreSQL and Redis are optional for local live search; see [backend setup](backend/README.md) to enable property history and background processing.

## Checks

```bash
npm test
npm run typecheck:haven
npm run build:haven
```

Backend checks, Docker setup, and API details are in [backend/README.md](backend/README.md).
