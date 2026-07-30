package server

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/skyoo2003/kvs"
)

const respOptWithScores = "WITHSCORES"

// respZAddOptions holds the flags ZADD accepts.
type respZAddOptions struct {
	nx      bool
	xx      bool
	changed bool
	greater bool
	less    bool
}

// parseZAddOptions consumes the leading flags and reports where the score and member pairs
// begin.
func parseZAddOptions(args [][]byte) (respZAddOptions, int, error) {
	var opts respZAddOptions

	at := 2
	for ; at < len(args); at++ {
		switch respUpper(args[at]) {
		case respOptNX:
			opts.nx = true
		case respOptXX:
			opts.xx = true
		case "CH":
			opts.changed = true
		case "GT":
			opts.greater = true
		case "LT":
			opts.less = true
		case respCmdIncr:
			return opts, 0, errRESPNoIncrOption
		default:
			// Anything else starts the score and member pairs.
			return opts, at, opts.validate()
		}
	}

	return opts, at, opts.validate()
}

func (o respZAddOptions) validate() error {
	if o.nx && (o.xx || o.greater || o.less) {
		return errRESPSyntax
	}
	if o.greater && o.less {
		return errRESPSyntax
	}

	return nil
}

// keeps reports whether the GT or LT flag makes the server hold on to the current score.
func (o respZAddOptions) keeps(current, next float64) bool {
	return (o.greater && next <= current) || (o.less && next >= current)
}

// apply writes the parsed pairs into zset, reporting how many members were new and how many
// existing scores it replaced.
func (o respZAddOptions) apply(zset *respZSet, pairs [][]byte, scores []float64) (added, changed int64) {
	for i, score := range scores {
		member := string(pairs[i*2+1])
		current, present := zset.score(member)

		switch {
		case (o.nx && present) || (o.xx && !present):
			continue
		case present && (o.keeps(current, score) || current == score):
			continue
		case present:
			changed++
		default:
			added++
		}

		zset.set(member, score)
	}

	return added, changed
}

func (c *respConn) cmdZAdd(args [][]byte) error {
	opts, at, err := parseZAddOptions(args)
	if err != nil {
		return c.writeFailure(err)
	}

	pairs := args[at:]
	if len(pairs) == 0 || len(pairs)%2 != 0 {
		return c.wrongArgs(string(args[0]))
	}

	// Parse every score before touching the store, so a bad argument cannot leave a
	// half-applied command behind.
	scores := make([]float64, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		score, scoreErr := respParseScore(pairs[i])
		if scoreErr != nil {
			return c.writeFailure(scoreErr)
		}
		scores = append(scores, score)
	}

	var added, changed int64
	if err := c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(string(args[1]))
		zset := newRESPZSet()
		if exists {
			stored, typeErr := respZSetOf(entry)
			if typeErr != nil {
				return typeErr
			}
			zset = stored
		}

		added, changed = opts.apply(zset, pairs, scores)
		respStoreCollection(tx, string(args[1]), entry, zset, zset.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if opts.changed {
		return c.writer.WriteInt(added + changed)
	}

	return c.writer.WriteInt(added)
}

func (c *respConn) cmdZRem(args [][]byte) error {
	var removed int64

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		zset, err := respZSetOf(entry)
		if err != nil {
			return err
		}

		for _, member := range args[2:] {
			if zset.remove(string(member)) {
				removed++
			}
		}
		respStoreCollection(tx, string(args[1]), entry, zset, zset.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(removed)
}

// cmdZScore handles ZSCORE and ZMSCORE, which differ only in how many members they take.
func (c *respConn) cmdZScore(args [][]byte) error {
	values := make([][]byte, 0, len(args)-2)

	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		for _, member := range args[2:] {
			score, present := zset.score(string(member))
			if !present {
				values = append(values, nil)

				continue
			}
			values = append(values, []byte(respFormatFloat(score)))
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if respUpper(args[0]) == respCmdZScore {
		return c.writer.WriteBulk(values[0])
	}

	return c.writeBulkArray(values)
}

func (c *respConn) cmdZCard(args [][]byte) error {
	var size int

	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}
		size = zset.len()

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(size))
}

func (c *respConn) cmdZCount(args [][]byte) error {
	low, high, err := respParseScoreRange(args[2], args[3])
	if err != nil {
		return c.writeFailure(err)
	}

	var count int64
	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		for _, score := range zset.members() {
			if low.atLeast(score) && high.atMost(score) {
				count++
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(count)
}

func (c *respConn) cmdZIncrBy(args [][]byte) error {
	delta, err := respParseScore(args[2])
	if err != nil {
		return c.writeFailure(err)
	}

	var result float64
	if err := c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(string(args[1]))
		zset := newRESPZSet()
		if exists {
			stored, typeErr := respZSetOf(entry)
			if typeErr != nil {
				return typeErr
			}
			zset = stored
		}

		current, _ := zset.score(string(args[3]))
		result = current + delta
		if math.IsNaN(result) {
			return errRESPNaNResult
		}
		zset.set(string(args[3]), result)
		respStoreCollection(tx, string(args[1]), entry, zset, zset.len())

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulkString(respFormatFloat(result))
}

// cmdZRange handles ZRANGE and ZREVRANGE, which take positions in the sorted order.
func (c *respConn) cmdZRange(args [][]byte) error {
	start, end, err := respParseIndexPair(args[2], args[3])
	if err != nil {
		return c.writeFailure(err)
	}

	withScores, err := respParseWithScores(args[4:])
	if err != nil {
		return c.writeFailure(err)
	}

	reverse := respUpper(args[0]) == respCmdZRevRange
	var items []string

	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		members := zset.sorted()
		if reverse {
			members = zset.reversed()
		}
		if from, to, ok := respRange(start, end, len(members)); ok {
			items = respWithScores(members[from:to], zset, withScores)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(items)
}

// cmdZRangeByScore handles ZRANGEBYSCORE and ZREVRANGEBYSCORE, which select by score. The
// reverse form takes its bounds highest first, as Redis does.
func (c *respConn) cmdZRangeByScore(args [][]byte) error {
	reverse := respUpper(args[0]) == respCmdZRevRangeByScore

	first, second := args[2], args[3]
	if reverse {
		first, second = second, first
	}
	low, high, err := respParseScoreRange(first, second)
	if err != nil {
		return c.writeFailure(err)
	}

	withScores, err := respParseWithScores(args[4:])
	if err != nil {
		return c.writeFailure(err)
	}

	var items []string
	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		selected := make([]string, 0, zset.len())
		for _, member := range zset.sorted() {
			if score, _ := zset.score(member); low.atLeast(score) && high.atMost(score) {
				selected = append(selected, member)
			}
		}
		if reverse {
			slices.Reverse(selected)
		}
		items = respWithScores(selected, zset, withScores)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(items)
}

// cmdZRank handles ZRANK and ZREVRANK.
func (c *respConn) cmdZRank(args [][]byte) error {
	reverse := respUpper(args[0]) == respCmdZRevRank
	rank := int64(-1)

	if err := c.read(func(tx *kvs.ReadTx) error {
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		members := zset.sorted()
		if reverse {
			members = zset.reversed()
		}
		if at := slices.Index(members, string(args[2])); at >= 0 {
			rank = int64(at)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if rank < 0 {
		return c.writer.WriteNull()
	}

	return c.writer.WriteInt(rank)
}

// cmdZScan walks a sorted set by member name rather than by score, because a cursor has to
// resume in an order that does not move when a score changes. The SCAN family promises no
// ordering, so this costs nothing a client is entitled to.
func (c *respConn) cmdZScan(args [][]byte) error {
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
		zset, err := respReadZSet(tx, string(args[1]))
		if err != nil {
			return err
		}

		page = respScanNames(slices.Collect(maps.Keys(zset.members())), after, resumed, opts,
			func(member string) []string {
				score, _ := zset.score(member)

				return []string{member, respFormatFloat(score)}
			})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeScanPage(cursor, page)
}

// respScoreBound is one end of a score range. Redis writes an exclusive bound with a
// leading parenthesis and accepts -inf and +inf.
type respScoreBound struct {
	value     float64
	exclusive bool
}

func (b respScoreBound) atLeast(score float64) bool {
	if b.exclusive {
		return score > b.value
	}

	return score >= b.value
}

func (b respScoreBound) atMost(score float64) bool {
	if b.exclusive {
		return score < b.value
	}

	return score <= b.value
}

func respParseScoreRange(low, high []byte) (lowBound, highBound respScoreBound, err error) {
	if lowBound, err = respParseScoreBound(low); err != nil {
		return lowBound, respScoreBound{}, err
	}

	highBound, err = respParseScoreBound(high)

	return lowBound, highBound, err
}

func respParseScoreBound(raw []byte) (respScoreBound, error) {
	text := string(raw)
	bound := respScoreBound{}
	if after, found := strings.CutPrefix(text, "("); found {
		bound.exclusive, text = true, after
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) {
		return bound, errRESPMinMaxFloat
	}
	bound.value = value

	return bound, nil
}

// respParseScore reads a ZADD score. Redis accepts the infinities but refuses NaN.
func respParseScore(raw []byte) (float64, error) {
	score, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsNaN(score) {
		return 0, errRESPNotFloat
	}

	return score, nil
}

func respParseWithScores(args [][]byte) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) != 1 || respUpper(args[0]) != respOptWithScores {
		return false, errRESPSyntax
	}

	return true, nil
}

// respWithScores interleaves each member with its score when the caller asked for scores.
func respWithScores(members []string, zset *respZSet, withScores bool) []string {
	if !withScores {
		return members
	}

	items := make([]string, 0, len(members)*2)
	for _, member := range members {
		score, _ := zset.score(member)
		items = append(items, member, respFormatFloat(score))
	}

	return items
}
