package server

import (
	"strconv"

	"github.com/skyoo2003/kvs"
)

// cmdPush handles LPUSH, RPUSH, LPUSHX, and RPUSHX.
func (c *respConn) cmdPush(args [][]byte) error {
	name := respUpper(args[0])
	atHead := name[0] == 'L'
	onlyIfPresent := name[len(name)-1] == 'X'

	values := make([]string, 0, len(args)-2)
	for _, value := range args[2:] {
		values = append(values, string(value))
	}

	var length int
	if err := c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(string(args[1]))
		if !exists && onlyIfPresent {
			return nil
		}

		list := newRESPList(nil)
		if exists {
			stored, err := respListOf(entry)
			if err != nil {
				return err
			}
			list = stored
		}

		if atHead {
			list.pushFront(values)
		} else {
			list.pushBack(values)
		}

		length = list.len()
		respStoreCollection(tx, string(args[1]), entry, list, length)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}

// cmdPop handles LPOP and RPOP, with or without a count. Without a count the reply is one
// bulk string; with one it is an array, so that a caller can tell the forms apart.
func (c *respConn) cmdPop(args [][]byte) error {
	count := 1
	if len(args) == 3 {
		parsed, err := strconv.Atoi(string(args[2]))
		if err != nil {
			return c.writer.WriteError(respErrNotInteger)
		}
		if parsed < 0 {
			return c.writeFailure(errRESPRange)
		}
		count = parsed
	}

	atHead := respUpper(args[0]) == respCmdLPop
	var popped []string

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		list, err := respListOf(entry)
		if err != nil {
			return err
		}

		if atHead {
			popped = list.popFront(count)
		} else {
			popped = list.popBack(count)
		}
		respStoreCollection(tx, string(args[1]), entry, list, list.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if len(args) == 2 {
		if len(popped) == 0 {
			return c.writer.WriteNull()
		}

		return c.writer.WriteBulkString(popped[0])
	}
	if popped == nil {
		return c.writer.WriteNullArray()
	}

	return c.writer.WriteStrings(popped)
}

func (c *respConn) cmdLRange(args [][]byte) error {
	start, end, err := respParseIndexPair(args[2], args[3])
	if err != nil {
		return c.writeFailure(err)
	}

	var items []string
	if err := c.read(func(tx *kvs.ReadTx) error {
		list, err := respReadList(tx, string(args[1]))
		if err != nil {
			return err
		}
		if from, to, ok := respRange(start, end, list.len()); ok {
			items = list.slice(from, to)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(items)
}

func (c *respConn) cmdLLen(args [][]byte) error {
	var length int

	if err := c.read(func(tx *kvs.ReadTx) error {
		list, err := respReadList(tx, string(args[1]))
		if err != nil {
			return err
		}
		length = list.len()

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}

func (c *respConn) cmdLIndex(args [][]byte) error {
	index, err := strconv.Atoi(string(args[2]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}

	var value []byte
	if err := c.read(func(tx *kvs.ReadTx) error {
		list, err := respReadList(tx, string(args[1]))
		if err != nil {
			return err
		}
		if at, ok := respIndex(index, list.len()); ok {
			value = []byte(list.at(at))
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(value)
}

func (c *respConn) cmdLSet(args [][]byte) error {
	index, err := strconv.Atoi(string(args[2]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return errRESPNoSuchKey
		}

		list, err := respListOf(entry)
		if err != nil {
			return err
		}

		at, inRange := respIndex(index, list.len())
		if !inRange {
			return errRESPIndexRange
		}
		list.set(at, string(args[3]))
		entry.Value = list
		tx.Set(string(args[1]), entry)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple(respOK)
}

// cmdLRem removes matching elements. A positive count works from the head, a negative one
// from the tail, and zero removes every match.
func (c *respConn) cmdLRem(args [][]byte) error {
	count, err := strconv.Atoi(string(args[2]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}

	var removed int
	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		list, err := respListOf(entry)
		if err != nil {
			return err
		}

		kept, dropped := respRemoveFromList(list.live(), string(args[3]), count)
		removed = dropped
		if dropped > 0 {
			list.replace(kept)
		}
		respStoreCollection(tx, string(args[1]), entry, list, list.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(removed))
}

func (c *respConn) cmdLTrim(args [][]byte) error {
	start, end, err := respParseIndexPair(args[2], args[3])
	if err != nil {
		return c.writeFailure(err)
	}

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		list, err := respListOf(entry)
		if err != nil {
			return err
		}

		trimmed := []string(nil)
		if from, to, inRange := respRange(start, end, list.len()); inRange {
			trimmed = list.slice(from, to)
		}
		list.replace(trimmed)
		respStoreCollection(tx, string(args[1]), entry, list, list.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple(respOK)
}

// respRemoveFromList drops elements equal to value, honoring the LREM count convention, and
// reports how many it removed.
func respRemoveFromList(list []string, value string, count int) (kept []string, removed int) {
	order := make([]int, 0, len(list))
	for i, item := range list {
		if item == value {
			order = append(order, i)
		}
	}

	limit := len(order)
	if count != 0 {
		limit = min(abs(count), limit)
		if count < 0 {
			// A negative count scans from the tail, so keep the last matches.
			order = order[len(order)-limit:]
		}
	}
	order = order[:limit]
	if limit == 0 {
		return list, 0
	}

	drop := make(map[int]struct{}, limit)
	for _, index := range order {
		drop[index] = struct{}{}
	}

	kept = make([]string, 0, len(list)-limit)
	for i, item := range list {
		if _, skip := drop[i]; !skip {
			kept = append(kept, item)
		}
	}

	return kept, limit
}

// respIndex resolves a single Redis index, where a negative value counts back from the end.
func respIndex(index, size int) (at int, ok bool) {
	if index < 0 {
		index += size
	}
	if index < 0 || index >= size {
		return 0, false
	}

	return index, true
}

func respParseIndexPair(first, second []byte) (start, end int, err error) {
	start, convErr := strconv.Atoi(string(first))
	if convErr != nil {
		return 0, 0, errRESPNotInteger
	}

	end, convErr = strconv.Atoi(string(second))
	if convErr != nil {
		return 0, 0, errRESPNotInteger
	}

	return start, end, nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
