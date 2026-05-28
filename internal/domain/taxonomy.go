package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Taxonomy holds the immutable whitelist of allowed categories and tags
// parsed from taxonomy.yml. Use LoadTaxonomy to construct.
type Taxonomy struct {
	Version  string
	paths    map[string]struct{}
	pathList []string
	tags     map[string]struct{}
	tagList  []string
}

type rawTaxonomy struct {
	Version    string    `yaml:"version"`
	Categories yaml.Node `yaml:"categories"`
	Tags       []string  `yaml:"tags"`
}

// LoadTaxonomy parses YAML bytes into a Taxonomy. Returns ErrTaxonomyMalformed
// (wrapped with parser detail) on any structural problem.
func LoadTaxonomy(data []byte) (Taxonomy, error) {
	var raw rawTaxonomy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Taxonomy{}, fmt.Errorf("%w: %w", ErrTaxonomyMalformed, err)
	}
	if raw.Version == "" {
		return Taxonomy{}, fmt.Errorf("%w: missing version", ErrTaxonomyMalformed)
	}
	paths, err := collectPaths(&raw.Categories, "")
	if err != nil {
		return Taxonomy{}, err
	}
	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}
	tagSet := make(map[string]struct{}, len(raw.Tags))
	for _, tag := range raw.Tags {
		tagSet[tag] = struct{}{}
	}
	return Taxonomy{
		Version:  raw.Version,
		paths:    pathSet,
		pathList: paths,
		tags:     tagSet,
		tagList:  slices.Clone(raw.Tags),
	}, nil
}

// collectPaths recursively walks the YAML node and yields every prefix path
// reachable from the root. A node may be a mapping (sub-tree), a sequence
// of leaf names, or null (a leaf with no further children).
func collectPaths(node *yaml.Node, prefix string) ([]string, error) {
	if node == nil || node.IsZero() {
		return nil, nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return collectPaths(node.Content[0], prefix)
	case yaml.MappingNode:
		var out []string
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			path := keyNode.Value
			if prefix != "" {
				path = prefix + "/" + keyNode.Value
			}
			out = append(out, path)
			children, err := collectPaths(valNode, path)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%w: sequence entries must be strings", ErrTaxonomyMalformed)
			}
			path := child.Value
			if prefix != "" {
				path = prefix + "/" + child.Value
			}
			out = append(out, path)
		}
		return out, nil
	case yaml.ScalarNode:
		if node.Value == "" || node.Tag == "!!null" {
			return nil, nil
		}
		// rare: a categories: bareString — treat as single leaf
		path := node.Value
		if prefix != "" {
			path = prefix + "/" + node.Value
		}
		return []string{path}, nil
	default:
		return nil, errors.New("unexpected yaml node kind")
	}
}

// AllCategoryPaths returns every valid category path in deterministic order.
// Used by the atomizer prompt to constrain LLM output.
func (t Taxonomy) AllCategoryPaths() []string {
	return slices.Clone(t.pathList)
}

// AllTags returns every valid tag in deterministic order.
func (t Taxonomy) AllTags() []string {
	return slices.Clone(t.tagList)
}

// CategoryAllowed reports whether path is exactly a valid category path.
// Comparison is case-sensitive; leading/trailing slashes are not stripped.
func (t Taxonomy) CategoryAllowed(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, ok := t.paths[path]
	return ok
}

// FilterTags returns only the tags present in the taxonomy whitelist,
// preserving input order and deduplicating.
func (t Taxonomy) FilterTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if _, dup := seen[tag]; dup {
			continue
		}
		if _, ok := t.tags[tag]; !ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Normalize applies taxonomy rules to a note:
//   - summary notes are always routed to CategorySummaries (their topical
//     category is intentionally ignored — atoms carry the topic, summaries
//     are meta-grouped by date)
//   - atoms with an invalid category fall back to CategoryUncategorized
//     (preserving the original in OriginalCategory)
//   - tags are filtered against the whitelist for both kinds
func (t Taxonomy) Normalize(n Note) Note {
	switch n.Kind {
	case KindSummary:
		n.Category = CategorySummaries
		n.OriginalCategory = ""
	default:
		if !t.CategoryAllowed(n.Category) {
			n.OriginalCategory = n.Category
			n.Category = CategoryUncategorized
		}
	}
	n.Tags = t.FilterTags(n.Tags)
	return n
}
