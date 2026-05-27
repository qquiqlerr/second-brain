package domain_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildID(t *testing.T) {
	moscow, _ := time.LoadLocation("Europe/Moscow")
	cases := []struct {
		name string
		date time.Time
		slug string
		want string
	}{
		{"basic", time.Date(2026, 5, 27, 22, 40, 0, 0, moscow), "tms-auth-bug", "20260527-tms-auth-bug"},
		{"midnight-moscow", time.Date(2026, 1, 1, 0, 0, 0, 0, moscow), "new-year", "20260101-new-year"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, domain.BuildID(c.date, c.slug))
		})
	}
}
