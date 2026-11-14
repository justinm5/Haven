package store

import (
	"context"
	"errors"
	"haven.local/backend/internal/intelligence"
)

var ErrStorageUnavailable = errors.New("property history storage is not configured")

// Unavailable provides honest empty read states when only live search is enabled.
// It cannot accept writes and never synthesizes property records.
type Unavailable struct{}

func (Unavailable) Ready(context.Context) error { return ErrStorageUnavailable }
func (Unavailable) Insert(context.Context, intelligence.Observation) (intelligence.Snapshot, error) {
	return intelligence.Snapshot{}, ErrStorageUnavailable
}
func (Unavailable) List(context.Context, int) ([]intelligence.Property, error) {
	return []intelligence.Property{}, nil
}
func (Unavailable) Report(context.Context, string) (intelligence.Report, error) {
	return intelligence.Report{}, ErrNotFound
}
func (Unavailable) Candidates(context.Context, intelligence.Address) ([]intelligence.Candidate, error) {
	return []intelligence.Candidate{}, nil
}
