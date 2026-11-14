-- Independent schema: does not change Haven/Supabase public tables.
CREATE SCHEMA IF NOT EXISTS haven;
CREATE TABLE IF NOT EXISTS haven.properties (
 id text PRIMARY KEY,
 canonical_key text NOT NULL UNIQUE,
 address jsonb NOT NULL,
 city text NOT NULL,
 state text NOT NULL,
 postal_code text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS properties_market ON haven.properties(city,state,postal_code);
CREATE TABLE IF NOT EXISTS haven.snapshots (
 id text PRIMARY KEY,
 property_id text NOT NULL REFERENCES haven.properties(id),
 source text NOT NULL,
 source_listing_id text NOT NULL,
 observed_at timestamptz NOT NULL,
 ingested_at timestamptz NOT NULL DEFAULT now(),
 payload jsonb NOT NULL,
 image_hashes text[] NOT NULL DEFAULT '{}',
 UNIQUE(source,source_listing_id,observed_at)
);
CREATE INDEX IF NOT EXISTS snapshots_timeline ON haven.snapshots(property_id,observed_at,id);
CREATE INDEX IF NOT EXISTS snapshots_images ON haven.snapshots USING gin(image_hashes);
-- Transactional outbox: creation is atomic with the immutable observation.
CREATE TABLE IF NOT EXISTS haven.jobs (
 snapshot_id text PRIMARY KEY REFERENCES haven.snapshots(id),
 available_at timestamptz NOT NULL DEFAULT now(),
 attempts int NOT NULL DEFAULT 0,
 last_error text,
 dead_at timestamptz
);
CREATE INDEX IF NOT EXISTS jobs_ready ON haven.jobs(available_at) WHERE dead_at IS NULL;
CREATE TABLE IF NOT EXISTS haven.reports (
 property_id text PRIMARY KEY REFERENCES haven.properties(id),
 payload jsonb NOT NULL,
 processed_at timestamptz NOT NULL DEFAULT now()
);
