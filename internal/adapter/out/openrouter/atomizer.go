package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// Atomizer asks an LLM (via OpenRouter chat completions) to split a dump
// into atomic notes. It implements port.out.Atomizer.
type Atomizer struct {
	client *Client
	model  string
}

// NewAtomizer constructs an Atomizer bound to a specific OpenRouter model.
func NewAtomizer(c *Client, model string) *Atomizer {
	return &Atomizer{client: c, model: model}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type atomItem struct {
	TitleSlug string   `json:"title_slug"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	Body      string   `json:"body"`
}

type atomizerResponse struct {
	Atoms   []atomItem `json:"atoms"`
	Summary *atomItem  `json:"summary"`
}

// Atomize sends one request to OpenRouter and returns the parsed notes.
// Returns domain.ErrAtomizerBadResponse if the response cannot be parsed.
func (a *Atomizer) Atomize(ctx context.Context, dump string, tax domain.Taxonomy) ([]domain.Note, error) {
	system := RenderAtomizeSystemPrompt(tax)
	reqBody := chatRequest{
		Model:       a.model,
		Temperature: 0.3,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: dump},
		},
	}

	var content string
	err := WithRetry(ctx, a.client.Retry, func(ctx context.Context) error {
		raw, err := a.postJSON(ctx, "/chat/completions", reqBody)
		if err != nil {
			return err
		}
		var resp chatResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("decode chat response: %w", err)
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("%w: no choices", domain.ErrAtomizerBadResponse)
		}
		content = resp.Choices[0].Message.Content
		return nil
	})
	if err != nil {
		return nil, err
	}

	resp, err := parseAtomizerResponse(content)
	if err != nil {
		return nil, err
	}
	notes := make([]domain.Note, 0, len(resp.Atoms)+1)
	for _, it := range resp.Atoms {
		notes = append(notes, domain.Note{
			Kind:     domain.KindAtom,
			Category: it.Category,
			Tags:     it.Tags,
			Slug:     it.TitleSlug,
			Body:     it.Body,
		})
	}
	if resp.Summary != nil && resp.Summary.TitleSlug != "" {
		notes = append(notes, domain.Note{
			Kind:     domain.KindSummary,
			Category: resp.Summary.Category,
			Tags:     resp.Summary.Tags,
			Slug:     resp.Summary.TitleSlug,
			Body:     resp.Summary.Body,
		})
	}
	return notes, nil
}

func parseAtomizerResponse(s string) (atomizerResponse, error) {
	s = strings.TrimSpace(s)
	s = stripCodeFence(s)
	var resp atomizerResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return atomizerResponse{}, fmt.Errorf("%w: %v", domain.ErrAtomizerBadResponse, err)
	}
	return resp, nil
}

// stripCodeFence removes a leading ```...``` markdown fence if the LLM
// added one despite instructions.
func stripCodeFence(s string) string {
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if cut, ok := strings.CutSuffix(strings.TrimRight(rest, "\n\r "), "```"); ok {
			return strings.TrimSpace(cut)
		}
	}
	return s
}

// postJSON sends a JSON body and returns the raw response, classifying
// non-2xx responses as HTTPError for the retry layer.
func (a *Atomizer) postJSON(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.client.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.client.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, HTTPError{Status: resp.StatusCode, Msg: string(data)}
	}
	return data, nil
}
