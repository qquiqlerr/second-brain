package domain_test

import (
	"os"
	"slices"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func loadTaxonomyFile(t *testing.T) domain.Taxonomy {
	t.Helper()
	data, err := os.ReadFile("../../testdata/taxonomy.yml.example")
	require.NoError(t, err)
	tax, err := domain.LoadTaxonomy(data)
	require.NoError(t, err)
	return tax
}

func TestLoadTaxonomy_Malformed(t *testing.T) {
	_, err := domain.LoadTaxonomy([]byte(":\nthis is not yaml"))
	require.ErrorIs(t, err, domain.ErrTaxonomyMalformed)
}

func TestTaxonomy_AllCategoryPaths(t *testing.T) {
	tax := loadTaxonomyFile(t)
	paths := tax.AllCategoryPaths()

	expected := []string{
		"work",
		"work/projects",
		"work/projects/tms",
		"work/projects/ingestion",
		"work/meetings",
		"work/notes",
		"personal",
		"personal/learning",
		"personal/finance",
		"personal/relationships",
		"health",
		"health/workouts",
		"health/nutrition",
		"health/sleep",
	}
	for _, p := range expected {
		require.Truef(t, slices.Contains(paths, p), "missing path %q", p)
	}
}

func TestTaxonomy_CategoryAllowed(t *testing.T) {
	tax := loadTaxonomyFile(t)
	require.True(t, tax.CategoryAllowed("work"))
	require.True(t, tax.CategoryAllowed("work/projects"))
	require.True(t, tax.CategoryAllowed("work/projects/tms"))
	require.True(t, tax.CategoryAllowed("health/sleep"))
	require.False(t, tax.CategoryAllowed("crypto/defi"))
	require.False(t, tax.CategoryAllowed(""))
	require.False(t, tax.CategoryAllowed("WORK")) // case-sensitive by design
}
