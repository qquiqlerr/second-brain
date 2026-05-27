package domain

import "errors"

// Sentinel errors returned by domain logic and the use case.
var (
	ErrEmptyDump           = errors.New("dump is empty after transcription")
	ErrTaxonomyMissing     = errors.New("taxonomy.yml not found")
	ErrTaxonomyMalformed   = errors.New("taxonomy.yml is malformed")
	ErrAtomizerBadResponse = errors.New("atomizer returned invalid response")
	ErrAtomizerNoNotes     = errors.New("atomizer returned zero notes")
)
