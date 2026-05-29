package openrouter

import (
	"net/http"
	"time"

	openroutersdk "github.com/OpenRouterTeam/go-sdk"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
)

// Client groups everything an OpenRouter-backed adapter needs: the official
// SDK for chat completions, plus the raw HTTP client and config for direct
// endpoints not yet covered by the SDK (audio transcription).
type Client struct {
	SDK     *openroutersdk.OpenRouter
	HTTP    *http.Client
	APIKey  string
	BaseURL string
	Retry   httpretry.Policy
}

// ClientConfig is the constructor input for New.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPReferer string
	XTitle      string
	HTTPTimeout time.Duration
	Retry       httpretry.Policy
}

// New constructs a Client. The same *http.Client is shared between the SDK
// and direct HTTP calls so timeouts and middleware behave consistently.
func New(cfg ClientConfig) *Client {
	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}

	opts := []openroutersdk.SDKOption{
		openroutersdk.WithSecurity(cfg.APIKey),
		openroutersdk.WithClient(httpClient),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, openroutersdk.WithServerURL(cfg.BaseURL))
	}
	if cfg.HTTPReferer != "" {
		opts = append(opts, openroutersdk.WithHTTPReferer(cfg.HTTPReferer))
	}
	if cfg.XTitle != "" {
		opts = append(opts, openroutersdk.WithXTitle(cfg.XTitle))
	}

	sdk := openroutersdk.New(opts...)

	return &Client{
		SDK:     sdk,
		HTTP:    httpClient,
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Retry:   cfg.Retry,
	}
}
