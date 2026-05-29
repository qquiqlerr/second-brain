// Package voyage implements the Embedder driven port using voyageai.com.
package voyage

import (
	"net/http"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
)

// Client groups HTTP + auth + retry policy for Voyage endpoints.
type Client struct {
	HTTP    *http.Client
	APIKey  string
	BaseURL string
	Retry   httpretry.Policy
}

// ClientConfig is the constructor input.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
	Retry       httpretry.Policy
}

// NewClient builds a Client. BaseURL omits trailing slash.
func NewClient(cfg ClientConfig) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: cfg.HTTPTimeout},
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Retry:   cfg.Retry,
	}
}
