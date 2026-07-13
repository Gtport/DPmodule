package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOverrides(t *testing.T) {
	t.Run("валидные правки", func(t *testing.T) {
		out, err := parseOverrides(map[string]string{"7": "7438-011-1234", "3": "5510-022-3456"})
		require.NoError(t, err)
		assert.Equal(t, map[int]string{7: "7438-011-1234", 3: "5510-022-3456"}, out)
	})

	t.Run("пустой индекс пропускается", func(t *testing.T) {
		out, err := parseOverrides(map[string]string{"7": ""})
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("некорректный ключ ord", func(t *testing.T) {
		_, err := parseOverrides(map[string]string{"abc": "7438-011-1234"})
		require.Error(t, err)
	})

	t.Run("неверный формат индекса", func(t *testing.T) {
		for _, bad := range []string{"7438-11-1234", "7438-011-123", "7438011234", "7438-011-12345", "abcd-011-1234"} {
			_, err := parseOverrides(map[string]string{"1": bad})
			assert.Error(t, err, "формат %q должен отклоняться", bad)
		}
	})

	t.Run("nil на входе", func(t *testing.T) {
		out, err := parseOverrides(nil)
		require.NoError(t, err)
		assert.Empty(t, out)
	})
}
