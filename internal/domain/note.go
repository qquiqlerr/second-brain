package domain

import "time"

// SchemaVersion is the YAML frontmatter schema version produced by MVP.
const SchemaVersion = "1.1"

// CategoryUncategorized is the fallback category used when an LLM proposes
// a category that does not pass the taxonomy whitelist.
const CategoryUncategorized = "uncategorized"

// CategorySummaries is the fixed category for KindSummary notes. Summaries
// are meta-notes (one per dump/day) and live in a dedicated directory
// regardless of the topical category of the underlying atoms.
const CategorySummaries = "summaries"

// Kind classifies notes produced by the pipeline.
const (
	KindAtom    = "atom"
	KindSummary = "summary"
)

// DumpSource identifies the origin of a dump entering the pipeline.
type DumpSource string

const (
	SourceTelegramText  DumpSource = "telegram-text"
	SourceTelegramVoice DumpSource = "telegram-voice"
)

// IngestMeta records pipeline trace information attached to every note.
type IngestMeta struct {
	DumpID          string `yaml:"dump_id"`
	ModelAtomize    string `yaml:"model_atomize"`
	ModelTranscribe string `yaml:"model_transcribe,omitempty"`
}

// Note is a single atomic thought as stored in the filesystem.
// Slug and Body are internal: Slug feeds into ID + filename, Body becomes
// the markdown content after the YAML frontmatter.
type Note struct {
	ID               string     `yaml:"id"`
	SchemaVersion    string     `yaml:"schema_version"`
	Date             time.Time  `yaml:"date"`
	Source           DumpSource `yaml:"source"`
	Kind             string     `yaml:"kind"`
	Category         string     `yaml:"category"`
	OriginalCategory string     `yaml:"original_category,omitempty"`
	Tags             []string   `yaml:"tags"`
	LinkedNotes      []string   `yaml:"linked_notes,omitempty"`
	Ingest           IngestMeta `yaml:"ingest"`
	Slug             string     `yaml:"-"`
	Body             string     `yaml:"-"`
}
