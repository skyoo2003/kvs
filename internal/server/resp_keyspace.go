package server

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skyoo2003/kvs"
)

const (
	respMatchAll = "*"
	respTypeNone = "none"

	// respScanBatch is how many keys one SCAN call inspects when the client names no COUNT,
	// matching the Redis default.
	respScanBatch = 10

	// respScanCursorLimit bounds how many unfinished iterations the server remembers, since an
	// abandoned cursor is otherwise never forgotten. One walk holds one entry.
	respScanCursorLimit = 1024

	// respRandomKeyTries is how many samples RANDOMKEY takes before reporting an empty
	// keyspace, since the cached key order still lists keys that expired without being
	// reclaimed. Redis gives up after the same number.
	respRandomKeyTries = 100
)

// respCursors remembers, per open cursor, the last name a scan reached.
//
// It belongs to the server rather than to the connection because client libraries pool
// connections: the SCAN that opens a cursor and the SCAN that continues it routinely land on
// different sockets, and per-connection state made the second one report an empty final page.
// A cursor is only a resume point, so it is harmless for any connection to present it.
type respCursors struct {
	mu sync.Mutex
	at map[uint64]string
}

// resume reports where a cursor left off. Cursor zero starts a fresh iteration.
func (r *respCursors) resume(cursor uint64) (after string, resumed, known bool) {
	if cursor == 0 {
		return "", false, true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	after, known = r.at[cursor]

	return after, known, known
}

// remember stores where a page stopped, reusing the handle when an iteration is already under
// way so a client sees one stable cursor per walk.
//
// A new handle is drawn at random rather than counted up. The table is server-wide, so a
// counter hands out the small numbers a client is most likely to present on its own, and
// presenting a live handle moves someone else's walk. A 64 bit draw makes that miss.
func (r *respCursors) remember(cursor uint64, resumeAfter string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.at == nil {
		r.at = make(map[uint64]string)
	}

	if cursor == 0 {
		// Abandoned iterations are never finished, so cap how many may pile up. Eviction picks
		// by map order and cannot tell an abandoned handle from a live one, which is why
		// presenting a forgotten cursor is an error rather than a finished page: the client
		// retries instead of believing its walk covered the keyspace.
		for id := range r.at {
			if len(r.at) < respScanCursorLimit {
				break
			}
			delete(r.at, id)
		}

		cursor = r.newHandle()
	}

	r.at[cursor] = resumeAfter

	return cursor
}

// newHandle draws an unused cursor. Zero is reserved for "start a fresh iteration", so it is
// never handed out.
func (r *respCursors) newHandle() uint64 {
	for {
		candidate := respRandomUint64()
		if _, taken := r.at[candidate]; candidate != 0 && !taken {
			return candidate
		}
	}
}

func (r *respCursors) forget(cursor uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.at, cursor)
}

// respExpireUnits maps each command of the EXPIRE family to the expiry unit it takes.
var respExpireUnits = map[string]string{
	respCmdExpire:    respUnitEX,
	respCmdPExpire:   respUnitPX,
	respCmdExpireAt:  respUnitEXAT,
	respCmdPExpireAt: respUnitPXAT,
}

func (c *respConn) cmdDel(args [][]byte) error {
	var removed int64

	if err := c.write(func(tx *kvs.Tx) error {
		for _, key := range args[1:] {
			if tx.Delete(string(key)) {
				removed++
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(removed)
}

func (c *respConn) cmdExists(args [][]byte) error {
	var found int64

	if err := c.read(func(tx *kvs.ReadTx) error {
		// A key repeated in the argument list is counted once per mention, as Redis does.
		for _, key := range args[1:] {
			if _, ok := tx.Get(string(key)); ok {
				found++
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(found)
}

func (c *respConn) cmdType(args [][]byte) error {
	name := respTypeNone

	if err := c.read(func(tx *kvs.ReadTx) error {
		if entry, ok := tx.Get(string(args[1])); ok {
			name = respTypeName(entry.Value)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple(name)
}

// cmdTTL handles TTL, which answers in seconds, and PTTL in milliseconds. A missing key
// answers -2 and a key without an expiry answers -1.
func (c *respConn) cmdTTL(args [][]byte) error {
	const (
		noKey    = -2
		noExpiry = -1
	)

	result := int64(noKey)

	if err := c.read(func(tx *kvs.ReadTx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}
		if entry.ExpiresAt.IsZero() {
			result = noExpiry

			return nil
		}

		remaining := entry.ExpiresAt.Sub(tx.Now())
		if strings.EqualFold(string(args[0]), respCmdPTTL) {
			result = remaining.Milliseconds()
		} else {
			// Redis rounds the remaining seconds up, so a key with 900ms left reports 1.
			result = int64(remaining.Round(time.Second) / time.Second)
			if result == 0 {
				result = 1
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(result)
}

// cmdExpire handles EXPIRE, PEXPIRE, EXPIREAT, and PEXPIREAT. The command name selects the
// unit and whether the argument is relative or absolute.
func (c *respConn) cmdExpire(args [][]byte) error {
	unit, ok := respExpireUnits[strings.ToUpper(string(args[0]))]
	if !ok {
		return c.writer.WriteError(respErrSyntax)
	}

	amount, err := strconv.ParseInt(string(args[2]), 10, 64)
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}
	if !respExpiryFits(unit, amount) {
		return c.writeFailure(errRESPExpireTime(string(args[0])))
	}

	var applied bool
	if err := c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(string(args[1]))
		if !exists {
			return nil
		}

		at, _ := respExpiryAt(unit, amount, tx.Now())
		// An expiry already in the past deletes the key outright, as Redis does.
		if !at.After(tx.Now()) {
			tx.Delete(string(args[1]))
			applied = true

			return nil
		}

		entry.ExpiresAt = at
		tx.Set(string(args[1]), entry)
		applied = true

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(applied))
}

func (c *respConn) cmdPersist(args [][]byte) error {
	var cleared bool

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok || entry.ExpiresAt.IsZero() {
			return nil
		}

		entry.ExpiresAt = time.Time{}
		tx.Set(string(args[1]), entry)
		cleared = true

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(cleared))
}

// cmdGetEx reads a key and adjusts its expiry in one step. PERSIST clears the expiry, and
// no option at all leaves it untouched.
func (c *respConn) cmdGetEx(args [][]byte) error {
	opts, persist, err := parseGetExOptions(args[2:])
	if err != nil {
		return c.writeFailure(err)
	}

	var value []byte
	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		stored, err := respStringOf(entry)
		if err != nil {
			return err
		}
		value = []byte(stored)

		switch at, hasExpiry := opts.expiresAt(tx.Now()); {
		case persist && !entry.ExpiresAt.IsZero():
			entry.ExpiresAt = time.Time{}
			tx.Set(string(args[1]), entry)
		case hasExpiry:
			entry.ExpiresAt = at
			tx.Set(string(args[1]), entry)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(value)
}

// parseGetExOptions reads the trailing options of GETEX, which are either PERSIST on its own
// or one expiry. The expiry parser is shared with SET, so the flags only SET takes are
// refused here rather than accepted and silently ignored, as "GETEX key EX 10 NX" was.
func parseGetExOptions(rest [][]byte) (opts respSetOptions, persist bool, err error) {
	if len(rest) == 0 {
		return opts, false, nil
	}

	if strings.EqualFold(string(rest[0]), respCmdPersist) {
		if len(rest) != 1 {
			return opts, false, errRESPSyntax
		}

		return opts, true, nil
	}

	if opts, err = parseSetOptions("GETEX", rest); err != nil {
		return opts, false, err
	}
	if opts.expiry == "" || opts.nx || opts.xx || opts.get || opts.keepTTL {
		return opts, false, errRESPSyntax
	}

	return opts, false, nil
}

// cmdRename handles RENAME and RENAMENX.
func (c *respConn) cmdRename(args [][]byte) error {
	onlyIfAbsent := strings.EqualFold(string(args[0]), respCmdRenameNX)
	source, target := string(args[1]), string(args[2])
	renamed := true

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(source)
		if !ok {
			return errRESPNoSuchKey
		}
		if _, exists := tx.Get(target); exists && onlyIfAbsent {
			renamed = false

			return nil
		}
		if source != target {
			tx.Set(target, entry)
			tx.Delete(source)
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if onlyIfAbsent {
		return c.writer.WriteInt(boolToInt(renamed))
	}

	return c.writer.WriteSimple(respOK)
}

func (c *respConn) cmdCopy(args [][]byte) error {
	replace := false
	if rest := args[3:]; len(rest) > 0 {
		if len(rest) != 1 || !strings.EqualFold(string(rest[0]), "REPLACE") {
			return c.writer.WriteError(respErrSyntax)
		}
		replace = true
	}

	var copied bool
	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}
		if _, exists := tx.Get(string(args[2])); exists && !replace {
			return nil
		}

		// Copy the container too, or both keys would share one collection.
		entry.Value = respCloneValue(entry.Value)
		tx.Set(string(args[2]), entry)
		copied = true

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(copied))
}

func (c *respConn) cmdKeys(args [][]byte) error {
	pattern := string(args[1])
	var keys []string

	if err := c.read(func(tx *kvs.ReadTx) error {
		keys = respFilterKeys(tx.Keys(), pattern)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteStrings(keys)
}

func (c *respConn) cmdRandomKey(_ [][]byte) error {
	var key string
	found := false

	if err := c.read(func(tx *kvs.ReadTx) error {
		// SortedKeys is cached, so picking an index costs nothing, where listing the keyspace
		// to take its first entry allocated a slice of every key on every call. The cached
		// order still lists keys that expired without being reclaimed, so skip past those.
		keys := tx.SortedKeys()
		if len(keys) == 0 {
			return nil
		}

		for range respRandomKeyTries {
			candidate := keys[respRandomUint64()%uint64(len(keys))]
			if _, live := tx.Get(candidate); live {
				key, found = candidate, true

				return nil
			}
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if !found {
		return c.writer.WriteNull()
	}

	return c.writer.WriteBulkString(key)
}

// cmdScan walks the keyspace a bounded page at a time, so a large keyspace no longer produces
// one enormous reply.
//
// The protocol treats a cursor as an opaque number, so kvs uses it as a handle to the last key
// a call reached and resumes after that key in sorted order. An index into the key order would
// have been simpler but unsound: deleting a key ahead of the cursor shifts everything down and
// the iteration would skip an element it was required to return.
func (c *respConn) cmdScan(args [][]byte) error {
	cursor, err := strconv.ParseUint(string(args[1]), 10, 64)
	if err != nil {
		return c.writeFailure(errRESPInvalidCursor)
	}

	opts, err := parseScanOptions(args[2:])
	if err != nil {
		return c.writeFailure(err)
	}

	after, resumed, known := c.scanResume(cursor)
	if !known {
		return c.writeFailure(errRESPInvalidCursor)
	}

	var page respScanPage
	if err := c.read(func(tx *kvs.ReadTx) error {
		page = respScanNames(tx.SortedKeys(), after, resumed, opts, func(key string) []string {
			// SortedKeys still lists a key that has expired without being reclaimed yet, so
			// the reply drops it here.
			entry, ok := tx.Get(key)
			if !ok {
				return nil
			}
			if opts.typeName != "" && respTypeName(entry.Value) != opts.typeName {
				return nil
			}

			return []string{key}
		})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeScanPage(cursor, page)
}

// respScanPage is one page of a scan. items holds the values the reply carries, which is one
// per element for a keyspace or set walk and a pair for a hash or sorted set. resumeAfter is
// the element name the next call continues past.
type respScanPage struct {
	items       []string
	resumeAfter string
	done        bool
}

// respScanNames walks names, which the caller must supply in sorted order, starting after the
// one the previous call reached, and inspects at most COUNT of them. emit turns one matching
// name into the values the reply carries, and returns nothing for a name that fails a filter of
// its own.
//
// COUNT bounds names examined rather than values returned, which is what Redis documents.
// The order is what makes the cursor resumable: it is the same on every call, so continuing
// past a name cannot skip or repeat one.
//
// SCAN takes its order from the store, which caches it, so walking a large keyspace sorts once
// rather than once per page. A collection walk sorts its own members instead.
// ponytail: per-page sort for HSCAN, SSCAN, and ZSCAN, worth a maintained order only if one
// hash, set, or sorted set ever grows large enough to matter.
func respScanNames(names []string, after string, resumed bool, opts respScanOptions,
	emit func(name string) []string,
) respScanPage {
	start := 0
	if resumed {
		start, _ = slices.BinarySearch(names, after)
		if start < len(names) && names[start] == after {
			start++
		}
	}

	end := min(start+opts.count, len(names))
	page := respScanPage{done: end >= len(names)}
	if end > start {
		page.resumeAfter = names[end-1]
	}

	for _, name := range names[start:end] {
		if respGlobMatch(opts.match, name) {
			page.items = append(page.items, emit(name)...)
		}
	}

	return page
}

func (c *respConn) scanResume(cursor uint64) (after string, resumed, known bool) {
	return c.server.cursors.resume(cursor)
}

func (c *respConn) writeScanPage(cursor uint64, page respScanPage) error {
	next := "0"
	if page.done {
		c.server.cursors.forget(cursor)
	} else {
		next = strconv.FormatUint(c.server.cursors.remember(cursor, page.resumeAfter), 10)
	}

	if err := c.writer.WriteArrayHeader(2); err != nil {
		return err
	}
	if err := c.writer.WriteBulkString(next); err != nil {
		return err
	}

	return c.writer.WriteStrings(page.items)
}

func (c *respConn) cmdDBSize(_ [][]byte) error {
	var size int

	if err := c.read(func(tx *kvs.ReadTx) error {
		size = tx.Len()

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(size))
}

// cmdFlush handles FLUSHDB and FLUSHALL. kvs holds one keyspace, so they do the same
// thing. An ASYNC or SYNC argument is accepted and ignored, since the flush is immediate.
func (c *respConn) cmdFlush(args [][]byte) error {
	if rest := args[1:]; len(rest) > 0 {
		if len(rest) != 1 || !isFlushMode(string(rest[0])) {
			return c.writer.WriteError(respErrSyntax)
		}
	}

	if err := c.write(func(tx *kvs.Tx) error {
		tx.Flush()

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple(respOK)
}

func isFlushMode(mode string) bool {
	return strings.EqualFold(mode, "ASYNC") || strings.EqualFold(mode, "SYNC")
}

// respCollectionLoader reads the container at key and reports its member names in sorted
// order together with the emit that turns one name into the values the reply carries.
type respCollectionLoader func(tx *kvs.ReadTx, key string) (names []string, emit func(string) []string, err error)

// runCollectionScan drives HSCAN, SSCAN, and ZSCAN, which differ only in the container they
// walk: they all place the key before the cursor and page it the same way.
func (c *respConn) runCollectionScan(args [][]byte, load respCollectionLoader) error {
	cursor, err := strconv.ParseUint(string(args[2]), 10, 64)
	if err != nil {
		return c.writeFailure(errRESPInvalidCursor)
	}

	opts, err := parseScanOptions(args[3:])
	if err != nil {
		return c.writeFailure(err)
	}

	after, resumed, known := c.scanResume(cursor)
	if !known {
		return c.writeFailure(errRESPInvalidCursor)
	}

	var page respScanPage
	if err := c.read(func(tx *kvs.ReadTx) error {
		names, emit, loadErr := load(tx, string(args[1]))
		if loadErr != nil {
			return loadErr
		}
		page = respScanNames(names, after, resumed, opts, emit)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writeScanPage(cursor, page)
}

// respScanOptions holds the trailing options shared by SCAN and its per-type variants.
type respScanOptions struct {
	match    string
	typeName string
	count    int
}

func parseScanOptions(args [][]byte) (respScanOptions, error) {
	opts := respScanOptions{match: respMatchAll, count: respScanBatch}

	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return opts, errRESPSyntax
		}

		switch strings.ToUpper(string(args[i])) {
		case "MATCH":
			opts.match = string(args[i+1])
		case "COUNT":
			count, err := strconv.Atoi(string(args[i+1]))
			if err != nil {
				return opts, errRESPNotInteger
			}
			if count < 1 {
				return opts, errRESPSyntax
			}
			opts.count = count
		case respCmdType:
			opts.typeName = strings.ToLower(string(args[i+1]))
		default:
			return opts, errRESPSyntax
		}
		i++
	}

	return opts, nil
}

func respFilterKeys(keys []string, pattern string) []string {
	if pattern == respMatchAll {
		return keys
	}

	matched := make([]string, 0, len(keys))
	for _, key := range keys {
		if respGlobMatch(pattern, key) {
			matched = append(matched, key)
		}
	}

	return matched
}
