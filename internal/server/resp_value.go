package server

import (
	"maps"

	"github.com/skyoo2003/kvs"
)

// Redis type names as TYPE reports them. The dynamic type of a stored value is what gives a
// key its type, so nothing has to be tracked alongside the value.
const (
	respTypeString = "string"
	respTypeHash   = "hash"
	respTypeList   = "list"
	respTypeSet    = "set"
	respTypeZSet   = "zset"
)

func respTypeName(value interface{}) string {
	switch value.(type) {
	case string:
		return respTypeString
	case *respList:
		return respTypeList
	case map[string]string:
		return respTypeHash
	case map[string]struct{}:
		return respTypeSet
	case *respZSet:
		return respTypeZSet
	default:
		return respTypeNone
	}
}

// respCloneValue copies a stored value far enough that the copy and the original can be
// changed independently. Strings are immutable, so they need no copy.
func respCloneValue(value interface{}) interface{} {
	switch stored := value.(type) {
	case *respList:
		return stored.clone()
	case map[string]string:
		return maps.Clone(stored)
	case map[string]struct{}:
		return maps.Clone(stored)
	case *respZSet:
		return stored.clone()
	default:
		return value
	}
}

// The loaders below read one container out of an entry, reporting WRONGTYPE when the key
// holds something else, the way Redis refuses a command aimed at the wrong type.

func respHashOf(entry kvs.Entry) (map[string]string, error) {
	value, ok := entry.Value.(map[string]string)
	if !ok {
		return nil, errRESPWrongType
	}

	return value, nil
}

func respListOf(entry kvs.Entry) (*respList, error) {
	value, ok := entry.Value.(*respList)
	if !ok {
		return nil, errRESPWrongType
	}

	return value, nil
}

func respSetOf(entry kvs.Entry) (map[string]struct{}, error) {
	value, ok := entry.Value.(map[string]struct{})
	if !ok {
		return nil, errRESPWrongType
	}

	return value, nil
}

func respZSetOf(entry kvs.Entry) (*respZSet, error) {
	value, ok := entry.Value.(*respZSet)
	if !ok {
		return nil, errRESPWrongType
	}

	return value, nil
}

// The readers below fetch a container for a read-only command. A missing key yields the zero
// container, which every read treats the way Redis treats an empty one.

func respReadHash(tx *kvs.ReadTx, key string) (map[string]string, error) {
	entry, ok := tx.Get(key)
	if !ok {
		return nil, nil
	}

	return respHashOf(entry)
}

func respReadList(tx *kvs.ReadTx, key string) (*respList, error) {
	entry, ok := tx.Get(key)
	if !ok {
		return nil, nil
	}

	return respListOf(entry)
}

func respReadSet(tx *kvs.ReadTx, key string) (map[string]struct{}, error) {
	entry, ok := tx.Get(key)
	if !ok {
		return nil, nil
	}

	return respSetOf(entry)
}

func respReadZSet(tx *kvs.ReadTx, key string) (*respZSet, error) {
	entry, ok := tx.Get(key)
	if !ok {
		return nil, nil
	}

	return respZSetOf(entry)
}

// respStoreCollection writes container back under key, deleting the key instead when the
// container has no elements left. Redis has no empty collections: losing the last element
// removes the key.
func respStoreCollection(tx *kvs.Tx, key string, entry kvs.Entry, container interface{}, size int) {
	if size == 0 {
		tx.Delete(key)

		return
	}

	entry.Value = container
	tx.Set(key, entry)
}
