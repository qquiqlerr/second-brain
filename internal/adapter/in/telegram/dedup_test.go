package telegram_test

import (
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/stretchr/testify/require"
)

func TestDedup_FirstTimeFalse_SubsequentTrue(t *testing.T) {
	d := telegram.NewUpdateDedup(4)
	require.False(t, d.Seen(1))
	require.True(t, d.Seen(1))
	require.False(t, d.Seen(2))
	require.True(t, d.Seen(2))
}

func TestDedup_EvictsOldest(t *testing.T) {
	d := telegram.NewUpdateDedup(2)
	d.Seen(1)
	d.Seen(2)
	d.Seen(3) // evicts 1
	require.False(t, d.Seen(1), "1 should have been evicted")
	require.True(t, d.Seen(2))
	require.True(t, d.Seen(3))
}
