package store

import (
	"context"
	"errors"
	"haven.local/backend/internal/intelligence"
	"reflect"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("observation identity already exists with different content")

type Repository interface {
	Insert(context.Context, intelligence.Observation) (intelligence.Snapshot, error)
	List(context.Context, int) ([]intelligence.Property, error)
	Report(context.Context, string) (intelligence.Report, error)
	Candidates(context.Context, intelligence.Address) ([]intelligence.Candidate, error)
	Ready(context.Context) error
}

type Memory struct {
	mu         sync.RWMutex
	snapshots  map[string]intelligence.Snapshot
	properties map[string]intelligence.Property
}

func NewMemory() *Memory {
	return &Memory{snapshots: map[string]intelligence.Snapshot{}, properties: map[string]intelligence.Property{}}
}
func normalizeObservation(o intelligence.Observation) intelligence.Observation {
	o.Address = intelligence.Normalize(o.Address)
	// PostgreSQL timestamps have microsecond precision. Normalize before hashing
	// so two identities cannot collide only after persistence.
	o.ObservedAt = o.ObservedAt.UTC().Truncate(time.Microsecond)
	hashes := append([]string{}, o.ImageHashes...)
	sort.Strings(hashes)
	o.ImageHashes = []string{}
	for _, h := range hashes {
		if len(o.ImageHashes) == 0 || h != o.ImageHashes[len(o.ImageHashes)-1] {
			o.ImageHashes = append(o.ImageHashes, h)
		}
	}
	return o
}
func (m *Memory) Ready(context.Context) error { return nil }
func (m *Memory) Insert(_ context.Context, o intelligence.Observation) (intelligence.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o = normalizeObservation(o)
	s := intelligence.Snapshot{ID: intelligence.ObservationID(o), PropertyID: intelligence.PropertyID(o.Address), Observation: o}
	if old, ok := m.snapshots[s.ID]; ok {
		if !reflect.DeepEqual(old, s) {
			return old, ErrConflict
		}
		return old, nil
	}
	m.snapshots[s.ID] = s
	m.properties[s.PropertyID] = intelligence.Property{ID: s.PropertyID, Address: o.Address, CanonicalKey: intelligence.Key(o.Address)}
	return s, nil
}
func (m *Memory) List(_ context.Context, limit int) ([]intelligence.Property, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []intelligence.Property{}
	for _, p := range m.properties {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *Memory) Candidates(ctx context.Context, a intelligence.Address) ([]intelligence.Candidate, error) {
	ps, err := m.List(ctx, 10000)
	if err != nil {
		return nil, err
	}
	return candidates(a, ps), nil
}
func candidates(a intelligence.Address, ps []intelligence.Property) []intelligence.Candidate {
	out := []intelligence.Candidate{}
	for _, p := range ps {
		score := intelligence.Similarity(a, p.Address)
		if score >= 0.65 {
			out = append(out, intelligence.Candidate{Property: p, Similarity: score, RequiresReview: intelligence.Key(a) != p.CanonicalKey, Reason: "Street trigram similarity; city, state, ZIP, house number and unit must agree. Similarity is not a probability."})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Similarity == out[j].Similarity {
			return out[i].Property.ID < out[j].Property.ID
		}
		return out[i].Similarity > out[j].Similarity
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}
func (m *Memory) Report(_ context.Context, id string) (intelligence.Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.properties[id]
	if !ok {
		return intelligence.Report{}, ErrNotFound
	}
	history, others := []intelligence.Snapshot{}, []intelligence.Snapshot{}
	for _, s := range m.snapshots {
		if s.PropertyID == id {
			history = append(history, s)
		} else {
			others = append(others, s)
		}
	}
	return makeReport(p, history, others), nil
}
func makeReport(p intelligence.Property, history, others []intelligence.Snapshot) intelligence.Report {
	sort.Slice(history, func(i, j int) bool {
		if history[i].ObservedAt.Equal(history[j].ObservedAt) {
			return history[i].ID < history[j].ID
		}
		return history[i].ObservedAt.Before(history[j].ObservedAt)
	})
	signals := intelligence.Analyze(p.ID, history, others)
	ids := map[string]bool{}
	for _, s := range signals {
		for _, id := range s.SnapshotIDs {
			ids[id] = true
		}
	}
	evidence := []intelligence.Snapshot{}
	for _, s := range append(append([]intelligence.Snapshot{}, history...), others...) {
		if ids[s.ID] {
			evidence = append(evidence, s)
		}
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ID < evidence[j].ID })
	return intelligence.Report{Property: p, Timeline: history, Signals: signals, Evidence: evidence, AnalyzedAt: time.Now().UTC()}
}
