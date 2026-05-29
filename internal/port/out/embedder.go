package out

import "context"

// EmbedKind selects the model's input_type hint. Voyage and most modern
// embedders produce different vectors for documents vs queries — using the
// right kind on each side measurably improves recall.
type EmbedKind string

const (
	EmbedDocument EmbedKind = "document"
	EmbedQuery    EmbedKind = "query"
)

// Embedder turns a batch of texts into dense vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string, kind EmbedKind) ([][]float32, error)
	Dim() int
}
