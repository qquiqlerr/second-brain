package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Embedder implements port/out.Embedder against the Voyage HTTP API.
type Embedder struct {
	client *Client
	model  string
	dim    int
}

// NewEmbedder constructs an Embedder bound to a model and expected dim.
func NewEmbedder(c *Client, model string, dim int) *Embedder {
	return &Embedder{client: c, model: model, dim: dim}
}

// Dim returns the configured embedding dimension.
func (e *Embedder) Dim() int { return e.dim }

type embedRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	InputType string   `json:"input_type"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
}

// Embed sends texts to Voyage and returns vectors in input order.
func (e *Embedder) Embed(ctx context.Context, texts []string, kind portout.EmbedKind) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	reqBody := embedRequest{
		Model:     e.model,
		Input:     texts,
		InputType: string(kind),
	}

	var resp embedResponse
	err := httpretry.With(ctx, e.client.Retry, func(ctx context.Context) error {
		raw, err := e.postJSON(ctx, "/embeddings", reqBody)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("%w: decode response: %w", domain.ErrEmbedder, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("%w: expected %d vectors, got %d", domain.ErrEmbedder, len(texts), len(resp.Data))
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("%w: out-of-range index %d", domain.ErrEmbedder, d.Index)
		}
		if len(d.Embedding) != e.dim {
			return nil, fmt.Errorf("%w: expected dim %d, got %d", domain.ErrEmbedder, e.dim, len(d.Embedding))
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

func (e *Embedder) postJSON(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", domain.ErrEmbedder, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.client.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.client.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, httpretry.HTTPError{Status: resp.StatusCode, Msg: string(data)}
	}
	return data, nil
}
