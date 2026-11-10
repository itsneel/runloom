package recordset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizedRoundTrip(t *testing.T) {
	v := map[string]any{"users": map[string]any{"a/b": map[string]any{"name": "Alice"}}, "events": []any{"one", "two"}, "empty": map[string]any{}, "nil": nil}
	r, err := Encode(v)
	require.NoError(t, err)
	require.Equal(t, json.RawMessage(`"one"`), r["events/array/00000000000000000000"])
	var actual map[string]any
	require.NoError(t, Decode(r, &actual))
	require.Equal(t, v, actual)
}

func TestJSONBKeyOrderingDoesNotRewriteUnchangedEntities(t *testing.T) {
	require.True(t, equal([]byte(`{"id":1,"name":"test"}`), []byte(`{"name":"test", "id":1}`)))
	require.False(t, equal([]byte(`{"id":9007199254740992}`), []byte(`{"id":9007199254740993}`)))
	require.False(t, equal(nil, []byte(`null`)))
}
