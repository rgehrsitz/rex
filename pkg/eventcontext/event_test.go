package eventcontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeFactEventAcceptsCanonicalFactObject(t *testing.T) {
	facts, metadata, enveloped, err := DecodeFactEvent([]byte(`{"temperature":35,"status":"hot"}`))
	require.NoError(t, err)
	assert.False(t, enveloped)
	assert.Equal(t, Metadata{}, metadata)
	assert.Equal(t, float64(35), facts["temperature"])
	assert.Equal(t, "hot", facts["status"])
}

func TestEncodeFactUpdatePreservesTraceAndIncrementsHop(t *testing.T) {
	ctx := WithMetadata(context.Background(), Metadata{TraceID: "event-7", Hop: 2})
	payload, err := EncodeFactUpdate(ctx, "status", "hot")
	require.NoError(t, err)

	facts, metadata, enveloped, err := DecodeFactEvent(payload)
	require.NoError(t, err)
	assert.True(t, enveloped)
	assert.Equal(t, Metadata{TraceID: "event-7", Hop: 3}, metadata)
	assert.Equal(t, "hot", facts["status"])
}

func TestDecodeFactEventRejectsMalformedEnvelope(t *testing.T) {
	_, _, _, err := DecodeFactEvent([]byte(`{"_rex":{"hop":1},"facts":{"status":"hot"}}`))
	require.ErrorContains(t, err, "missing trace_id")
}
