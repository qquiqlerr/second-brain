// Package openaiembed implements port/out.Embedder against any OpenAI-compatible
// /embeddings endpoint (OpenAI itself, OpenRouter, or any other vendor that
// mirrors the OpenAI shape).
package openaiembed

import (
	"net/http"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
)

// Client groups HTTP + auth + retry policy for an OpenAI-compatible endpoint.
type Client struct {
	HTTP    *http.Client
	APIKey  string
	BaseURL string // e.g. https://openrouter.ai/api/v1
	Retry   httpretry.Policy
}

// ClientConfig is the constructor input.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
	Retry       httpretry.Policy
}

// NewClient builds a Client. BaseURL must omit trailing slash.
func NewClient(cfg ClientConfig) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: cfg.HTTPTimeout},
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Retry:   cfg.Retry,
	}
}
