package indexer

import (
	"testing"

	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/stretchr/testify/assert"
)

func TestDiff_NewFileGoesToEmbed(t *testing.T) {
	fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h1"}}
	db := []portout.IndexMeta{}
	toEmbed, toDelete := Diff(fs, db)
	assert.Equal(t, []string{"a"}, ids(toEmbed))
	assert.Empty(t, toDelete)
}

func TestDiff_UnchangedFileSkipped(t *testing.T) {
	fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h1"}}
	db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
	toEmbed, toDelete := Diff(fs, db)
	assert.Empty(t, toEmbed)
	assert.Empty(t, toDelete)
}

func TestDiff_ChangedHashGoesToEmbed(t *testing.T) {
	fs := []FSEntry{{ID: "a", FilePath: "a.md", BodyHash: "h2"}}
	db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
	toEmbed, toDelete := Diff(fs, db)
	assert.Equal(t, []string{"a"}, ids(toEmbed))
	assert.Empty(t, toDelete)
}

func TestDiff_MissingFileGoesToDelete(t *testing.T) {
	fs := []FSEntry{}
	db := []portout.IndexMeta{{ID: "a", BodyHash: "h1"}}
	toEmbed, toDelete := Diff(fs, db)
	assert.Empty(t, toEmbed)
	assert.Equal(t, []string{"a"}, toDelete)
}

func ids(entries []FSEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
