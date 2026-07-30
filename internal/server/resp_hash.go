package server

import (
	"maps"
	"slices"
	"strconv"

	"github.com/skyoo2003/kvs"
)

// respWriteHash loads the hash at key for a mutating command, returning an empty hash when
// the key is absent. The entry comes back too so that the key keeps its expiry.
func respWriteHash(tx *kvs.Tx, key string) (map[string]string, kvs.Entry, error) {
	entry, ok := tx.Get(key)
	if !ok {
		return make(map[string]string), kvs.Entry{}, nil
	}

	hash, err := respHashOf(entry)
	if err != nil {
		return nil, entry, err
	}

	return hash, entry, nil
}

func (c *respConn) cmdHSet(args [][]byte) error {
	fields := args[2:]
	if len(fields)%2 != 0 {
		return c.wrongArgs(string(args[0]))
	}

	var added int64
	if err := c.write(func(tx *kvs.Tx) error {
		hash, entry, err := respWriteHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		for i := 0; i < len(fields); i += 2 {
			field := string(fields[i])
			if _, exists := hash[field]; !exists {
				added++
			}
			hash[field] = string(fields[i+1])
		}
		respStoreCollection(tx, string(args[1]), entry, hash, len(hash))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(added)
}

func (c *respConn) cmdHSetNX(args [][]byte) error {
	var added bool

	if err := c.write(func(tx *kvs.Tx) error {
		hash, entry, err := respWriteHash(tx, string(args[1]))
		if err != nil {
			return err
		}
		if _, exists := hash[string(args[2])]; exists {
			return nil
		}

		hash[string(args[2])] = string(args[3])
		respStoreCollection(tx, string(args[1]), entry, hash, len(hash))
		added = true

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(added))
}

func (c *respConn) cmdHGet(args [][]byte) error {
	var value []byte

	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}
		if stored, ok := hash[string(args[2])]; ok {
			value = []byte(stored)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(value)
}

func (c *respConn) cmdHMGet(args [][]byte) error {
	values := make([][]byte, 0, len(args)-2)

	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		for _, field := range args[2:] {
			stored, ok := hash[string(field)]
			if !ok {
				values = append(values, nil)

				continue
			}
			values = append(values, []byte(stored))
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeBulkArray(values)
}

func (c *respConn) cmdHDel(args [][]byte) error {
	var removed int64

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		hash, err := respHashOf(entry)
		if err != nil {
			return err
		}

		for _, field := range args[2:] {
			if _, exists := hash[string(field)]; exists {
				delete(hash, string(field))
				removed++
			}
		}
		respStoreCollection(tx, string(args[1]), entry, hash, len(hash))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(removed)
}

// cmdHGetAll answers HGETALL, HKEYS, and HVALS, which differ only in which half of each
// pair they report.
func (c *respConn) cmdHGetAll(args [][]byte) error {
	name := respUpper(args[0])
	var items []string

	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		items = make([]string, 0, len(hash)*2)
		for field, value := range hash {
			switch name {
			case respCmdHKeys:
				items = append(items, field)
			case respCmdHVals:
				items = append(items, value)
			default:
				items = append(items, field, value)
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(items)
}

func (c *respConn) cmdHLen(args [][]byte) error {
	var length int

	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}
		length = len(hash)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}

func (c *respConn) cmdHExists(args [][]byte) error {
	var exists bool

	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}
		_, exists = hash[string(args[2])]

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(exists))
}

func (c *respConn) cmdHIncrBy(args [][]byte) error {
	delta, err := strconv.ParseInt(string(args[3]), 10, 64)
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}

	var result int64
	if err := c.write(func(tx *kvs.Tx) error {
		hash, entry, err := respWriteHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		current := int64(0)
		if stored, ok := hash[string(args[2])]; ok {
			if current, err = strconv.ParseInt(stored, 10, 64); err != nil {
				return errRESPHashNotInteger
			}
		}
		if addOverflows(current, delta) {
			return errRESPOverflow
		}

		result = current + delta
		hash[string(args[2])] = strconv.FormatInt(result, 10)
		respStoreCollection(tx, string(args[1]), entry, hash, len(hash))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(result)
}

func (c *respConn) cmdHIncrByFloat(args [][]byte) error {
	delta, err := strconv.ParseFloat(string(args[3]), 64)
	if err != nil {
		return c.writer.WriteError(errRESPNotFloat.Error())
	}

	var result string
	if err := c.write(func(tx *kvs.Tx) error {
		hash, entry, err := respWriteHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		current := float64(0)
		if stored, ok := hash[string(args[2])]; ok {
			if current, err = strconv.ParseFloat(stored, 64); err != nil {
				return errRESPHashNotFloat
			}
		}

		result = respFormatFloat(current + delta)
		hash[string(args[2])] = result
		respStoreCollection(tx, string(args[1]), entry, hash, len(hash))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulkString(result)
}

func (c *respConn) cmdHScan(args [][]byte) error {
	cursor, opts, err := c.parseCollectionScan(args)
	if err != nil {
		return c.writeFailure(err)
	}

	after, resumed, known := c.scanResume(cursor)
	if !known {
		return c.writeScanPage(cursor, respScanPage{done: true})
	}

	var page respScanPage
	if err := c.read(func(tx *kvs.ReadTx) error {
		hash, err := respReadHash(tx, string(args[1]))
		if err != nil {
			return err
		}

		page = respScanNames(slices.Collect(maps.Keys(hash)), after, resumed, opts,
			func(field string) []string { return []string{field, hash[field]} })

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeScanPage(cursor, page)
}
