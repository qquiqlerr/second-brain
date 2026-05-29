// Package httpretry provides retry policy and HTTP error classification
// shared by all HTTP-backed driven adapters.
package httpretry

import "fmt"

// HTTPError is returned by adapter callbacks to communicate an HTTP-shaped
// failure to the retry layer without coupling to a specific client.
type HTTPError struct {
	Status int
	Msg    string
}

func (e HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Msg) }
