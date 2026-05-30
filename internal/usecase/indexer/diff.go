// Package indexer maintains the vector index by periodically diffing the
// filesystem against the persisted index.
package indexer

import (
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// FSEntry is what a filesystem scan yields per file.
type FSEntry struct {
	ID       string
	FilePath string
	BodyHash string
}

// Diff returns (toEmbed, toDelete) — IDs that need a fresh embedding,
// and IDs that have been removed from disk and should leave the index.
func Diff(fs []FSEntry, db []portout.IndexMeta) (toEmbed []FSEntry, toDelete []string) {
	dbByID := make(map[string]portout.IndexMeta, len(db))
	for _, m := range db {
		dbByID[m.ID] = m
	}
	fsByID := make(map[string]struct{}, len(fs))
	for _, e := range fs {
		fsByID[e.ID] = struct{}{}
		existing, ok := dbByID[e.ID]
		if !ok || existing.BodyHash != e.BodyHash {
			toEmbed = append(toEmbed, e)
		}
	}
	for id := range dbByID {
		if _, ok := fsByID[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	return toEmbed, toDelete
}
