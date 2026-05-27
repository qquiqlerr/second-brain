// Package out declares driven (outbound) ports the use case requires.
package out

import (
	"context"
	"io"
)

// Transcriber converts an audio stream into raw text.
type Transcriber interface {
	Transcribe(ctx context.Context, audio io.Reader, mime string) (string, error)
}
