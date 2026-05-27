package domain

import (
	"github.com/gosimple/slug"
)

// MaxSlugLength caps the slug to keep filenames manageable on all filesystems.
const MaxSlugLength = 40

// FallbackSlug is used when input normalizes to an empty string.
const FallbackSlug = "n-a"

// Slugify normalizes a free-form title into an ASCII kebab-case slug.
// It transliterates non-ASCII characters (e.g. Cyrillic), lowercases,
// replaces whitespace and punctuation with hyphens, and caps the length.
func Slugify(s string) string {
	out := slug.Make(s)
	if out == "" {
		return FallbackSlug
	}
	if len(out) > MaxSlugLength {
		out = out[:MaxSlugLength]
		// trim trailing hyphen if cut landed on it
		for len(out) > 0 && out[len(out)-1] == '-' {
			out = out[:len(out)-1]
		}
	}
	return out
}
