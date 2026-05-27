package fs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/stretchr/testify/require"
)

const sampleTaxonomy = `version: "1.0"
categories:
  work:
    - alpha
    - beta
tags:
  - bug
`

func TestTaxonomyLoader_LoadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taxonomy.yml")
	require.NoError(t, os.WriteFile(path, []byte(sampleTaxonomy), 0o644))

	loader := fs.NewTaxonomyLoader(path, 50*time.Millisecond)

	tax1, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.True(t, tax1.CategoryAllowed("work/alpha"))

	// rewrite to a different content; within TTL, cache returns old result
	require.NoError(t, os.WriteFile(path, []byte(`version: "1.0"
categories:
  health:
    - sleep
tags: []
`), 0o644))

	tax2, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.True(t, tax2.CategoryAllowed("work/alpha"), "cached snapshot expected within TTL")

	time.Sleep(80 * time.Millisecond)
	// after TTL expires + mtime changed: new content loaded
	tax3, err := loader.Load(t.Context())
	require.NoError(t, err)
	require.False(t, tax3.CategoryAllowed("work/alpha"))
	require.True(t, tax3.CategoryAllowed("health/sleep"))
}

func TestTaxonomyLoader_MissingFile(t *testing.T) {
	loader := fs.NewTaxonomyLoader("/no/such/path/taxonomy.yml", time.Second)
	_, err := loader.Load(t.Context())
	require.Error(t, err)
}
