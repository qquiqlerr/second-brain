package openrouter

import (
	_ "embed"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

//go:embed prompts/atomize_system.txt
var atomizeSystemPromptTpl string

// RenderAtomizeSystemPrompt substitutes taxonomy data into the prompt template.
// The output is suitable as a system message for the chat completion request.
func RenderAtomizeSystemPrompt(tax domain.Taxonomy) string {
	cats := strings.Join(tax.AllCategoryPaths(), ", ")
	tags := strings.Join(tax.AllTags(), ", ")
	out := strings.ReplaceAll(atomizeSystemPromptTpl, "{{categories}}", cats)
	out = strings.ReplaceAll(out, "{{tags}}", tags)
	return out
}
