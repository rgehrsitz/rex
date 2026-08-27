package eventcontext

import (
	"context"
	"encoding/json"
	"fmt"
)

const envelopeMetadataKey = "_rex"

type envelope struct {
	Metadata Metadata               `json:"_rex"`
	Facts    map[string]interface{} `json:"facts"`
}

// DecodeFactEvent accepts the canonical fact-object event format and the
// metadata envelope used for Rex-derived events. The boolean reports whether
// the payload used the envelope format.
func DecodeFactEvent(payload []byte) (map[string]interface{}, Metadata, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, Metadata{}, false, err
	}

	metadataRaw, enveloped := raw[envelopeMetadataKey]
	if !enveloped {
		var facts map[string]interface{}
		if err := json.Unmarshal(payload, &facts); err != nil {
			return nil, Metadata{}, false, err
		}
		return facts, Metadata{}, false, nil
	}

	if _, ok := raw["facts"]; !ok {
		return nil, Metadata{}, true, fmt.Errorf("event envelope is missing facts")
	}

	var metadata Metadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return nil, Metadata{}, true, fmt.Errorf("decode event metadata: %w", err)
	}
	if metadata.TraceID == "" {
		return nil, Metadata{}, true, fmt.Errorf("event envelope is missing trace_id")
	}
	if metadata.Hop < 0 {
		return nil, Metadata{}, true, fmt.Errorf("event envelope has a negative hop")
	}

	var facts map[string]interface{}
	if err := json.Unmarshal(raw["facts"], &facts); err != nil {
		return nil, Metadata{}, true, fmt.Errorf("decode event facts: %w", err)
	}
	if facts == nil {
		return nil, Metadata{}, true, fmt.Errorf("event envelope facts must be an object")
	}
	return facts, metadata, true, nil
}

// EncodeFactUpdate uses the canonical object format for independent updates.
// When the context belongs to an event chain, it emits an envelope with the
// next hop so derived updates remain correlated and bounded.
func EncodeFactUpdate(ctx context.Context, key string, value interface{}) ([]byte, error) {
	metadata, ok := MetadataFromContext(ctx)
	if !ok {
		return json.Marshal(map[string]interface{}{key: value})
	}

	metadata.Hop++
	return json.Marshal(envelope{
		Metadata: metadata,
		Facts:    map[string]interface{}{key: value},
	})
}
