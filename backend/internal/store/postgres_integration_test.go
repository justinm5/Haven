package store

import (
	"context"
	"errors"
	"fmt"
	"haven.local/backend/internal/intelligence"
	"os"
	"sync"
	"testing"
	"time"
)

// CI supplies a dedicated disposable database. No tests connect to a default
// DSN or to the existing application's Supabase instance.
func TestPostgresPipeline(t *testing.T) {
	dsn := os.Getenv("HAVEN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set HAVEN_TEST_DATABASE_URL to a dedicated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	p, err := Open(ctx, dsn, os.Getenv("HAVEN_TEST_REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for i := 0; i < 2; i++ {
		if err = p.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	o := sample()
	nonce := fmt.Sprint(time.Now().UnixNano())
	o.SourceListingID = "integration-" + nonce
	o.Address.Street = nonce + " Synthetic Street"
	o.ImageHashes = []string{intelligence.Digest(nonce)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := p.Insert(ctx, o); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	s, err := p.Insert(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Report(ctx, s.PropertyID); !errors.Is(err, ErrPending) {
		t.Fatalf("new report should be pending: %v", err)
	}
	var snapshots, jobs int
	if err = p.Pool.QueryRow(ctx, `SELECT count(*) FROM haven.snapshots WHERE id=$1`, s.ID).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err = p.Pool.QueryRow(ctx, `SELECT count(*) FROM haven.jobs WHERE snapshot_id=$1`, s.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 || jobs != 1 {
		t.Fatalf("non-idempotent snapshots=%d jobs=%d", snapshots, jobs)
	}
	b := o
	b.SourceListingID += "-other"
	b.Address.Street = nonce + " Other Road"
	other, err := p.Insert(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 100; i++ {
			did, e := p.ProcessOne(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if !did {
				return
			}
		}
		t.Fatal("queue did not drain")
	}
	drain()
	for _, id := range []string{s.PropertyID, other.PropertyID} {
		r, e := p.Report(ctx, id)
		if e != nil || len(r.Evidence) != 2 || len(r.Signals) != 1 || r.ProcessingPending {
			t.Fatalf("bad shared image materialization: %v %+v", e, r)
		}
	}
	// Import an older observation after the newest one. The worker must use
	// event time, preserve both, and invalidate any Redis-cached report.
	old := o
	old.ObservedAt = o.ObservedAt.Add(-24 * time.Hour)
	old.RentCents = 300000
	if _, err = p.Insert(ctx, old); err != nil {
		t.Fatal(err)
	}
	drain()
	r, err := p.Report(ctx, s.PropertyID)
	if err != nil || len(r.Timeline) != 2 {
		t.Fatalf("late event lost: %v %+v", err, r)
	}
	found := false
	for _, signal := range r.Signals {
		if signal.Kind == "rent_changed" {
			found = true
		}
	}
	if !found {
		t.Fatal("late event did not trigger price history analysis")
	}
	changed := o
	changed.Description = "identity conflict"
	if _, err = p.Insert(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("immutable observation overwritten: %v", err)
	}
}
