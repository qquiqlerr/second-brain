package openrouter_test

import (
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRenderAtomizePrompt_InlinesCategoriesAndTags(t *testing.T) {
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    - tms
tags:
  - bug
  - idea
`))
	require.NoError(t, err)

	out := openrouter.RenderAtomizeSystemPrompt(tax)
	require.True(t, strings.Contains(out, "work"))
	require.True(t, strings.Contains(out, "work/tms"))
	require.True(t, strings.Contains(out, "bug"))
	require.True(t, strings.Contains(out, "idea"))
	require.False(t, strings.Contains(out, "{{categories}}"), "placeholder must be replaced")
	require.False(t, strings.Contains(out, "{{tags}}"), "placeholder must be replaced")
}
