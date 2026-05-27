package domain_test

import (
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"TMS Auth Bug", "tms-auth-bug"},
		{"  spaces  around  ", "spaces-around"},
		{"моя идея", "moia-ideia"},
		{"привет, мир!", "privet-mir"},
		{"already-kebab", "already-kebab"},
		{"!!!", "n-a"},
		{"", "n-a"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			require.Equal(t, c.want, domain.Slugify(c.in))
		})
	}
}

func TestSlugify_LengthCap(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := domain.Slugify(long)
	require.LessOrEqual(t, len(got), 40)
}
