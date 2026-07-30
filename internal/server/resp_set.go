package server

import (
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"

	"github.com/skyoo2003/kvs"
)

func (c *respConn) cmdSAdd(args [][]byte) error {
	var added int64

	if err := c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(string(args[1]))
		set := make(map[string]struct{})
		if exists {
			stored, err := respSetOf(entry)
			if err != nil {
				return err
			}
			set = stored
		}

		for _, member := range args[2:] {
			if _, present := set[string(member)]; present {
				continue
			}
			set[string(member)] = struct{}{}
			added++
		}
		respStoreCollection(tx, string(args[1]), entry, set, len(set))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(added)
}

func (c *respConn) cmdSRem(args [][]byte) error {
	var removed int64

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		set, err := respSetOf(entry)
		if err != nil {
			return err
		}

		for _, member := range args[2:] {
			if _, present := set[string(member)]; present {
				delete(set, string(member))
				removed++
			}
		}
		respStoreCollection(tx, string(args[1]), entry, set, len(set))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(removed)
}

func (c *respConn) cmdSMembers(args [][]byte) error {
	var members []string

	if err := c.read(func(tx *kvs.ReadTx) error {
		set, err := respReadSet(tx, string(args[1]))
		if err != nil {
			return err
		}
		members = slices.Collect(maps.Keys(set))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(members)
}

func (c *respConn) cmdSIsMember(args [][]byte) error {
	var present bool

	if err := c.read(func(tx *kvs.ReadTx) error {
		set, err := respReadSet(tx, string(args[1]))
		if err != nil {
			return err
		}
		_, present = set[string(args[2])]

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(present))
}

func (c *respConn) cmdSCard(args [][]byte) error {
	var size int

	if err := c.read(func(tx *kvs.ReadTx) error {
		set, err := respReadSet(tx, string(args[1]))
		if err != nil {
			return err
		}
		size = len(set)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(size))
}

func (c *respConn) cmdSPop(args [][]byte) error {
	count, hasCount, err := respParseOptionalCount(args)
	if err != nil {
		return c.writeFailure(err)
	}

	var taken []string
	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		set, err := respSetOf(entry)
		if err != nil {
			return err
		}

		// Go randomizes where a map range starts, so taking members in range order already
		// picks arbitrary ones.
		for member := range set {
			if len(taken) == count {
				break
			}
			taken = append(taken, member)
		}
		for _, member := range taken {
			delete(set, member)
		}
		respStoreCollection(tx, string(args[1]), entry, set, len(set))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if !hasCount {
		if len(taken) == 0 {
			return c.writer.WriteNull()
		}

		return c.writer.WriteBulkString(taken[0])
	}

	return c.writer.WriteStrings(taken)
}

// cmdSRandMember reads members without removing them. A negative count allows the same
// member to come back more than once, as Redis does.
func (c *respConn) cmdSRandMember(args [][]byte) error {
	count := 1
	hasCount := len(args) == 3
	if hasCount {
		parsed, err := strconv.Atoi(string(args[2]))
		if err != nil {
			return c.writer.WriteError(respErrNotInteger)
		}
		count = parsed
	}

	var members []string
	if err := c.read(func(tx *kvs.ReadTx) error {
		set, err := respReadSet(tx, string(args[1]))
		if err != nil {
			return err
		}
		members = slices.Collect(maps.Keys(set))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	picked := respPickMembers(members, count)
	if !hasCount {
		if len(picked) == 0 {
			return c.writer.WriteNull()
		}

		return c.writer.WriteBulkString(picked[0])
	}

	return c.writer.WriteStrings(picked)
}

// cmdSetOp handles SUNION, SINTER, and SDIFF.
func (c *respConn) cmdSetOp(args [][]byte) error {
	name := respUpper(args[0])
	var result []string

	if err := c.read(func(tx *kvs.ReadTx) error {
		sets := make([]map[string]struct{}, 0, len(args)-1)
		for _, key := range args[1:] {
			set, err := respReadSet(tx, string(key))
			if err != nil {
				return err
			}
			sets = append(sets, set)
		}

		result = respCombineSets(name, sets)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(result)
}

func (c *respConn) cmdSScan(args [][]byte) error {
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
		set, err := respReadSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		page = respScanNames(slices.Collect(maps.Keys(set)), after, resumed, opts,
			func(member string) []string { return []string{member} })

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeScanPage(cursor, page)
}

// respCombineSets applies the named set operation against the first set.
func respCombineSets(name string, sets []map[string]struct{}) []string {
	if len(sets) == 0 {
		return nil
	}

	result := maps.Clone(sets[0])
	if result == nil {
		result = make(map[string]struct{})
	}

	for _, other := range sets[1:] {
		switch name {
		case respCmdSUnion:
			maps.Copy(result, other)
		case respCmdSInter:
			for member := range result {
				if _, present := other[member]; !present {
					delete(result, member)
				}
			}
		default: // SDIFF
			for member := range other {
				delete(result, member)
			}
		}
	}

	return slices.Collect(maps.Keys(result))
}

// respPickMembers selects count members. A negative count is a request for that many picks
// with repetition allowed.
func respPickMembers(members []string, count int) []string {
	if len(members) == 0 || count == 0 {
		return nil
	}

	if count > 0 {
		return members[:min(count, len(members))]
	}

	picked := make([]string, 0, -count)
	for range -count {
		// Which member comes back is arbitrary by design, so an unseeded generator is the
		// right tool here.
		picked = append(picked, members[rand.IntN(len(members))]) //nolint:gosec // arbitrary pick, not a security decision
	}

	return picked
}

// respParseOptionalCount reads the optional non-negative count that SPOP takes.
func respParseOptionalCount(args [][]byte) (count int, present bool, err error) {
	if len(args) != 3 {
		return 1, false, nil
	}

	parsed, convErr := strconv.Atoi(string(args[2]))
	if convErr != nil {
		return 0, true, errRESPNotInteger
	}
	if parsed < 0 {
		return 0, true, errRESPRange
	}

	return parsed, true, nil
}
