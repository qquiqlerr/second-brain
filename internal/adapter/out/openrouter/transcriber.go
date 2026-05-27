package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Transcriber calls OpenRouter's /audio/transcriptions endpoint. The endpoint
// is OpenAI-compatible (multipart with "file" + "model"), but is not yet
// covered by the SDK; we issue the request via the SDK's shared HTTP client.
type Transcriber struct {
	client *Client
	model  string
}

// NewTranscriber constructs a Transcriber bound to a specific model.
func NewTranscriber(c *Client, model string) *Transcriber {
	return &Transcriber{client: c, model: model}
}

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe uploads audio bytes to the configured model and returns the
// transcript. The mime parameter is informational; the multipart "filename"
// is derived from it but Whisper accepts a wide range of formats including ogg.
func (t *Transcriber) Transcribe(ctx context.Context, audio io.Reader, mimeType string) (string, error) {
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)

	filename := filenameForMIME(mimeType)
	fw, err := mp.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("multipart file: %w", err)
	}
	if _, err := io.Copy(fw, audio); err != nil {
		return "", fmt.Errorf("copy audio: %w", err)
	}
	if err := mp.WriteField("model", t.model); err != nil {
		return "", fmt.Errorf("multipart model field: %w", err)
	}
	if err := mp.Close(); err != nil {
		return "", fmt.Errorf("multipart close: %w", err)
	}

	var transcript string
	err = WithRetry(ctx, t.client.Retry, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.client.BaseURL+"/audio/transcriptions", bytes.NewReader(body.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+t.client.APIKey)
		req.Header.Set("Content-Type", mp.FormDataContentType())

		resp, err := t.client.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			return HTTPError{Status: resp.StatusCode, Msg: string(raw)}
		}
		var parsed transcribeResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("decode transcribe response: %w", err)
		}
		transcript = parsed.Text
		return nil
	})
	return transcript, err
}

func filenameForMIME(m string) string {
	switch m {
	case "audio/ogg", "audio/opus":
		return "voice.ogg"
	case "audio/mpeg", "audio/mp3":
		return "voice.mp3"
	case "audio/wav":
		return "voice.wav"
	default:
		return "voice.bin"
	}
}
