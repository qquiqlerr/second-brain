package out

import (
	"context"
	"time"
)

// IndexItem is what the indexer upserts into the vector store.
// LinkedNotes is intentionally absent — linker updates that field via
// UpdateLinkedNotes so re-embedding never clobbers existing links.
type IndexItem struct {
	ID        string
	FilePath  string
	BodyHash  string
	Category  string
	Tags      []string
	Date      time.Time
	Kind      string // "atom" | "summary"
	Embedding []float32
}

// IndexMeta is the readable row plus linked_notes, returned by GetMeta and
// ListAllMeta. It is the source of truth for the YAML linked_notes field.
type IndexMeta struct {
	ID          string
	FilePath    string
	BodyHash    string
	Category    string
	Tags        []string
	Date        time.Time
	Kind        string
	IndexedAt   time.Time
	LinkedNotes []string
}

// SearchQuery scopes a vector search. Zero values mean "no filter".
type SearchQuery struct {
	TopK           int
	KindFilter     []string  // e.g. []string{"atom"}
	CategoryPrefix string    // e.g. "work/projects"
	DateFrom       time.Time // zero = no lower bound
}

// SearchHit is one result. Score is cosine similarity in [-1, 1].
type SearchHit struct {
	ID    string
	Score float32
	Meta  IndexMeta
}

// VectorIndex is the driven port for the embedding store.
type VectorIndex interface {
	Upsert(ctx context.Context, items []IndexItem) error
	SearchByVector(ctx context.Context, vec []float32, q SearchQuery) ([]SearchHit, error)
	GetMeta(ctx context.Context, id string) (IndexMeta, bool, error)
	GetEmbedding(ctx context.Context, id string) ([]float32, bool, error)
	UpdateLinkedNotes(ctx context.Context, id string, links []string) error
	DeleteByIDs(ctx context.Context, ids []string) error
	ListAllMeta(ctx context.Context) ([]IndexMeta, error)
}
