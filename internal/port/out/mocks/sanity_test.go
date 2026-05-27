package mocks_test

import (
	"testing"

	outmocks "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTranscriberMock_Smoke(t *testing.T) {
	m := outmocks.NewMockTranscriber(t)
	m.EXPECT().Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("hello", nil).Once()

	got, err := m.Transcribe(t.Context(), nil, "audio/ogg")
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}
