package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/skyoo2003/kvs"
)

// respCodec persists the value types the RESP commands store. It lives here rather than beside
// the log because those types are unexported: the root package cannot encode what it cannot
// name.
type respCodec struct{}

// respStored tags an encoded value with the Redis type it came from, which is what tells the
// decoder which container to rebuild. TYPE already derives that name from the dynamic type, so
// the log reuses the same names rather than inventing a second vocabulary for the same things.
type respStored struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

func (respCodec) Encode(value interface{}) ([]byte, error) {
	stored := respStored{Type: respTypeName(value)}

	switch typed := value.(type) {
	case string:
		stored.Value = typed
	case *respList:
		// live() rather than the backing array: the space a list reserves at its head is an
		// allocation strategy, not data, and it costs nothing to rebuild.
		stored.Value = typed.live()
	case map[string]string:
		stored.Value = typed
	case map[string]struct{}:
		// Sorted, so that rewriting a set nobody changed produces the bytes it produced last
		// time.
		stored.Value = slices.Sorted(maps.Keys(typed))
	case *respZSet:
		// Only the scores. The member order a sorted set caches is derived from them, so it
		// is rebuilt by the first range query after a restart.
		stored.Value = typed.members()
	default:
		return nil, fmt.Errorf("%w: %T", kvs.ErrUnsupportedValue, value)
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", stored.Type, err)
	}

	return data, nil
}

// Clone defers to the copy the commands already use when they need a value they can change
// without disturbing the one in the store.
func (respCodec) Clone(value interface{}) interface{} {
	return respCloneValue(value)
}

func (respCodec) Decode(data []byte) (interface{}, error) {
	var stored struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("decode stored value: %w", err)
	}

	switch stored.Type {
	case respTypeString:
		return respDecode[string](stored.Value, stored.Type)
	case respTypeHash:
		return respDecode[map[string]string](stored.Value, stored.Type)
	case respTypeList:
		values, err := respDecode[[]string](stored.Value, stored.Type)
		if err != nil {
			return nil, err
		}

		return newRESPList(values), nil
	case respTypeSet:
		members, err := respDecode[[]string](stored.Value, stored.Type)
		if err != nil {
			return nil, err
		}

		set := make(map[string]struct{}, len(members))
		for _, member := range members {
			set[member] = struct{}{}
		}

		return set, nil
	case respTypeZSet:
		scores, err := respDecode[map[string]float64](stored.Value, stored.Type)
		if err != nil {
			return nil, err
		}

		zset := newRESPZSet()
		for member, score := range scores {
			zset.set(member, score)
		}

		return zset, nil
	}

	return nil, fmt.Errorf("%w: %q", kvs.ErrUnsupportedValue, stored.Type)
}

func respDecode[T any](raw json.RawMessage, name string) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode %s: %w", name, err)
	}

	return value, nil
}
