package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"haven.local/backend/internal/intelligence"
	"haven.local/backend/migrations"
	"log/slog"
	"reflect"
	"time"
)

type Postgres struct {
	Pool  *pgxpool.Pool
	Cache *redis.Client
}

func Open(ctx context.Context, dsn, redisURL string) (*Postgres, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	p := &Postgres{Pool: pool}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if redisURL != "" {
		opt, e := redis.ParseURL(redisURL)
		if e != nil {
			pool.Close()
			return nil, e
		}
		opt.DialTimeout = time.Second
		opt.ReadTimeout = time.Second
		opt.WriteTimeout = time.Second
		p.Cache = redis.NewClient(opt)
	}
	return p, nil
}
func (p *Postgres) Close() {
	p.Pool.Close()
	if p.Cache != nil {
		_ = p.Cache.Close()
	}
}
func (p *Postgres) Ready(ctx context.Context) error { return p.Pool.Ping(ctx) }
func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.Pool.Exec(ctx, migrations.Initial)
	return err
}
func (p *Postgres) Insert(ctx context.Context, o intelligence.Observation) (intelligence.Snapshot, error) {
	o = normalizeObservation(o)
	s := intelligence.Snapshot{ID: intelligence.ObservationID(o), PropertyID: intelligence.PropertyID(o.Address), Observation: o}
	payload, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	address, _ := json.Marshal(o.Address)
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return s, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO haven.properties(id,canonical_key,address,city,state,postal_code) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, s.PropertyID, intelligence.Key(o.Address), address, o.Address.City, o.Address.State, o.Address.PostalCode)
	if err != nil {
		return s, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO haven.snapshots(id,property_id,source,source_listing_id,observed_at,payload,image_hashes) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, s.ID, s.PropertyID, o.Source, o.SourceListingID, o.ObservedAt, payload, o.ImageHashes)
	if err != nil {
		return s, err
	}
	if tag.RowsAffected() == 0 {
		var raw []byte
		var old intelligence.Snapshot
		if err = tx.QueryRow(ctx, `SELECT payload FROM haven.snapshots WHERE id=$1`, s.ID).Scan(&raw); err != nil {
			return s, err
		}
		if err = json.Unmarshal(raw, &old); err != nil {
			return s, err
		}
		if !reflect.DeepEqual(old, s) {
			return old, ErrConflict
		}
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO haven.jobs(snapshot_id) VALUES($1)`, s.ID)
		if err != nil {
			return s, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return s, err
	}
	// Redis is a wake-up hint, never the sole record of queued work. Loss of a
	// hint is safe because workers poll the durable PostgreSQL outbox.
	if p.Cache != nil {
		hintCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_ = p.Cache.LPush(hintCtx, "haven:jobs:wake", s.ID).Err()
		_ = p.Cache.LTrim(hintCtx, "haven:jobs:wake", 0, 999).Err()
	}
	return s, nil
}
func (p *Postgres) List(ctx context.Context, limit int) ([]intelligence.Property, error) {
	rows, err := p.Pool.Query(ctx, `SELECT id,canonical_key,address FROM haven.properties ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []intelligence.Property{}
	for rows.Next() {
		var prop intelligence.Property
		var raw []byte
		if err = rows.Scan(&prop.ID, &prop.CanonicalKey, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &prop.Address); err != nil {
			return nil, err
		}
		out = append(out, prop)
	}
	return out, rows.Err()
}
func (p *Postgres) Candidates(ctx context.Context, a intelligence.Address) ([]intelligence.Candidate, error) {
	a = intelligence.Normalize(a)
	rows, err := p.Pool.Query(ctx, `SELECT id,canonical_key,address FROM haven.properties WHERE city=$1 AND state=$2 AND postal_code=$3 ORDER BY id LIMIT 1000`, a.City, a.State, a.PostalCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps := []intelligence.Property{}
	for rows.Next() {
		var prop intelligence.Property
		var raw []byte
		if err = rows.Scan(&prop.ID, &prop.CanonicalKey, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &prop.Address); err != nil {
			return nil, err
		}
		ps = append(ps, prop)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return candidates(a, ps), nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func reportFromDB(ctx context.Context, q querier, id string) (intelligence.Report, error) {
	var prop intelligence.Property
	var address []byte
	if err := q.QueryRow(ctx, `SELECT id,canonical_key,address FROM haven.properties WHERE id=$1`, id).Scan(&prop.ID, &prop.CanonicalKey, &address); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return intelligence.Report{}, ErrNotFound
		}
		return intelligence.Report{}, err
	}
	if err := json.Unmarshal(address, &prop.Address); err != nil {
		return intelligence.Report{}, err
	}
	rows, err := q.Query(ctx, `SELECT payload FROM haven.snapshots WHERE property_id=$1 ORDER BY observed_at,id`, id)
	if err != nil {
		return intelligence.Report{}, err
	}
	history := []intelligence.Snapshot{}
	hashes := []string{}
	for rows.Next() {
		var raw []byte
		var s intelligence.Snapshot
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return intelligence.Report{}, err
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			rows.Close()
			return intelligence.Report{}, err
		}
		history = append(history, s)
		hashes = append(hashes, s.ImageHashes...)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return intelligence.Report{}, err
	}
	others := []intelligence.Snapshot{}
	if len(hashes) > 0 {
		rows, err = q.Query(ctx, `SELECT payload FROM haven.snapshots WHERE property_id<>$1 AND image_hashes && $2::text[] ORDER BY id`, id, hashes)
		if err != nil {
			return intelligence.Report{}, err
		}
		for rows.Next() {
			var raw []byte
			var s intelligence.Snapshot
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return intelligence.Report{}, err
			}
			if err = json.Unmarshal(raw, &s); err != nil {
				rows.Close()
				return intelligence.Report{}, err
			}
			others = append(others, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return intelligence.Report{}, err
		}
	}
	return makeReport(prop, history, others), nil
}
func (p *Postgres) Report(ctx context.Context, id string) (intelligence.Report, error) {
	// Reports are materialized asynchronously. Pending is explicit (no invented
	// clean result), and Redis entries expire quickly even if invalidation fails.
	if p.Cache != nil {
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		raw, err := p.Cache.Get(cctx, "haven:report:"+id).Bytes()
		cancel()
		if err == nil {
			var r intelligence.Report
			if json.Unmarshal(raw, &r) == nil {
				return p.reportStatus(ctx, r)
			}
		}
	}
	var raw []byte
	err := p.Pool.QueryRow(ctx, `SELECT payload FROM haven.reports WHERE property_id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if e := p.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM haven.properties WHERE id=$1)`, id).Scan(&exists); e != nil {
			return intelligence.Report{}, e
		}
		if exists {
			return intelligence.Report{}, ErrPending
		}
		return intelligence.Report{}, ErrNotFound
	}
	if err != nil {
		return intelligence.Report{}, err
	}
	var r intelligence.Report
	if err = json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if p.Cache != nil {
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_ = p.Cache.Set(cctx, "haven:report:"+id, raw, 15*time.Second).Err()
		cancel()
	}
	return p.reportStatus(ctx, r)
}
func (p *Postgres) reportStatus(ctx context.Context, r intelligence.Report) (intelligence.Report, error) {
	err := p.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM haven.jobs j JOIN haven.snapshots s ON s.id=j.snapshot_id WHERE s.property_id=$1)`, r.Property.ID).Scan(&r.ProcessingPending)
	return r, err
}

var ErrPending = errors.New("analysis pending")

// ProcessOne holds a job row lock through the materialization transaction.
// Other workers skip it; a crash rolls back both work and acknowledgement.
func (p *Postgres) ProcessOne(ctx context.Context) (bool, error) {
	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var snapshotID, propertyID string
	var hashes []string
	err = tx.QueryRow(ctx, `SELECT j.snapshot_id,s.property_id,s.image_hashes FROM haven.jobs j JOIN haven.snapshots s ON s.id=j.snapshot_id WHERE j.dead_at IS NULL AND j.available_at<=now() ORDER BY j.available_at,j.snapshot_id FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(&snapshotID, &propertyID, &hashes)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Serialize materialization across workers. This pilot-level lock ensures
	// shared-image findings stay symmetric; partition locks when scaling.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728491)`); err != nil {
		return true, err
	}
	ids := []string{propertyID}
	rows, err := tx.Query(ctx, `SELECT DISTINCT property_id FROM haven.snapshots WHERE property_id<>$1 AND image_hashes && $2::text[] ORDER BY property_id`, propertyID, hashes)
	if err != nil {
		return true, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return true, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	for _, id := range ids {
		report, e := reportFromDB(ctx, tx, id)
		if e != nil {
			err = e
			break
		}
		var raw []byte
		raw, err = json.Marshal(report)
		if err != nil {
			break
		}
		_, err = tx.Exec(ctx, `INSERT INTO haven.reports(property_id,payload) VALUES($1,$2) ON CONFLICT(property_id) DO UPDATE SET payload=excluded.payload,processed_at=now()`, id, raw)
		if err != nil {
			break
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		// Back off on poison work and retain it for inspection after eight attempts.
		_, retryErr := p.Pool.Exec(ctx, `UPDATE haven.jobs SET attempts=attempts+1,last_error=$2,available_at=now()+make_interval(secs=>LEAST(300,power(2,attempts+1)::int)),dead_at=CASE WHEN attempts+1>=8 THEN now() ELSE NULL END WHERE snapshot_id=$1`, snapshotID, err.Error())
		if retryErr != nil {
			return true, fmt.Errorf("process: %v; retry update: %w", err, retryErr)
		}
		return true, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM haven.jobs WHERE snapshot_id=$1`, snapshotID); err != nil {
		return true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return true, err
	}
	if p.Cache != nil {
		keys := []string{}
		for _, id := range ids {
			keys = append(keys, "haven:report:"+id)
		}
		cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		_ = p.Cache.Del(cctx, keys...).Err()
		cancel()
	}
	return true, nil
}
func (p *Postgres) RunWorker(ctx context.Context) {
	for ctx.Err() == nil {
		workCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		did, err := p.ProcessOne(workCtx)
		cancel()
		if err != nil {
			slog.Error("analysis job failed", "error", err)
		}
		if did && err == nil {
			continue
		}
		if p.Cache != nil {
			_, wakeErr := p.Cache.BRPop(ctx, time.Second, "haven:jobs:wake").Result()
			if wakeErr != nil && !errors.Is(wakeErr, redis.Nil) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}
