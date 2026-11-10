package prompts

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTags(t *testing.T) {
	tags, err := NormalizeTags([]string{" Support ", "BILLING", "support", "customer care", "客服"})
	require.NoError(t, err)
	require.Equal(t, []string{"support", "billing", "customer care", "客服"}, tags)
	_, err = NormalizeTags([]string{strings.Repeat("😀", 40)})
	require.NoError(t, err)
	for _, value := range []string{"", " ", "bad,tag", "bad\x00tag", "bad\u0085tag", strings.Repeat("😀", 41)} {
		_, err := NormalizeTags([]string{value})
		require.Error(t, err)
	}
	many := []string{}
	for i := 0; i < 21; i++ {
		many = append(many, fmt.Sprintf("tag-%d", i))
	}
	_, err = NormalizeTags(many)
	require.Error(t, err)
}
