package server

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/pkg/resp"
)

const (
	respOptGet     = "GET"
	respOptKeepTTL = "KEEPTTL"
	respOptNX      = "NX"
	respOptXX      = "XX"

	// Expiry units, shared by SET, GETEX, SETEX, and the EXPIRE family.
	respUnitEX   = "EX"
	respUnitPX   = "PX"
	respUnitEXAT = "EXAT"
	respUnitPXAT = "PXAT"

	respErrStringTooLong = "ERR string exceeds maximum allowed size (proto-max-bulk-len)"
)

// Expiry amounts are bounded so that turning a relative expiry into a time.Duration cannot
// overflow: a Duration is int64 nanoseconds, which runs out after about 292 years. Without the
// bound "EXPIRE key 9223372036854775807" wrapped into the past and deleted the key instead.
const (
	respMaxExpirySeconds = math.MaxInt64 / int64(time.Second)
	respMaxExpiryMillis  = math.MaxInt64 / int64(time.Millisecond)
)

var (
	errRESPSyntax     = errors.New(respErrSyntax)
	errRESPNotInteger = errors.New(respErrNotInteger)
	errRESPNotFloat   = errors.New("ERR value is not a valid float")
	errRESPWrongType  = errors.New(respErrWrongType)
	errRESPOverflow   = errors.New("ERR increment or decrement would overflow")
	errRESPRange      = errors.New("ERR value is out of range, must be positive")
	errRESPNoSuchKey  = errors.New("ERR no such key")

	errRESPStringTooLong = errors.New(respErrStringTooLong)

	errRESPInvalidCursor  = errors.New("ERR invalid cursor")
	errRESPHashNotInteger = errors.New("ERR hash value is not an integer")
	errRESPHashNotFloat   = errors.New("ERR hash value is not a float")
	errRESPIndexRange     = errors.New("ERR index out of range")
	errRESPMinMaxFloat    = errors.New("ERR min or max is not a float")
	errRESPNoIncrOption   = errors.New("ERR ZADD with the INCR option is not supported")
	errRESPNaNResult      = errors.New("ERR resulting score is not a number (NaN)")

	// errRESPWrongPass and errRESPNoPassword carry protocol messages rather than Go error
	// messages, so they keep the exact text and trailing punctuation Redis sends.
	errRESPWrongPass  error = respWireError(respErrWrongPass)
	errRESPNoPassword error = respWireError(
		"ERR Client sent AUTH, but no password is set. Did you mean AUTH <username> <password>?",
	)
)

// respWireError is a reply the client sees verbatim, carried as an error so that a parsing
// helper can hand it back to the command that will write it.
type respWireError string

func (e respWireError) Error() string {
	return string(e)
}

// errRESPExpireTime is what Redis answers when a command carries an expiry it cannot use,
// naming the command that carried it. Every command taking an expiry reports it the same way,
// so that a client branching on the text is not told two different stories.
func errRESPExpireTime(command string) error {
	return respWireError("ERR invalid expire time in '" + strings.ToLower(command) + "' command")
}

// respStringOf returns the string a key holds, refusing keys that hold another type the
// way Redis does.
func respStringOf(entry kvs.Entry) (string, error) {
	value, ok := entry.Value.(string)
	if !ok {
		return "", errRESPWrongType
	}

	return value, nil
}

// respExpiryAt turns one of the EX, PX, EXAT, or PXAT options into an absolute instant.
func respExpiryAt(unit string, value int64, now time.Time) (time.Time, bool) {
	switch unit {
	case respUnitEX:
		return now.Add(time.Duration(value) * time.Second), true
	case respUnitPX:
		return now.Add(time.Duration(value) * time.Millisecond), true
	case respUnitEXAT:
		return time.Unix(value, 0), true
	case respUnitPXAT:
		return time.UnixMilli(value), true
	default:
		return time.Time{}, false
	}
}

// respExpiryFits reports whether value is a usable amount for unit. A value beyond the range
// wraps into the past once converted, which would silently drop the key.
func respExpiryFits(unit string, value int64) bool {
	limit := respMaxExpirySeconds
	if unit == respUnitPX || unit == respUnitPXAT {
		limit = respMaxExpiryMillis
	}

	return value >= -limit && value <= limit
}

// respSetOptions holds the parsed trailing options of SET. The expiry is kept as the raw
// unit and amount so that it can be resolved against the transaction's own clock.
type respSetOptions struct {
	nx        bool
	xx        bool
	get       bool
	keepTTL   bool
	expiry    string
	expiryArg int64
}

// parseSetOptions reads the trailing options of SET. command names the caller, since GETEX
// shares this parser and an unusable expiry has to name the command the client actually sent.
func parseSetOptions(command string, args [][]byte) (respSetOptions, error) {
	var opts respSetOptions

	for i := 0; i < len(args); i++ {
		switch name := strings.ToUpper(string(args[i])); name {
		case respOptNX:
			opts.nx = true
		case respOptXX:
			opts.xx = true
		case respOptGet:
			opts.get = true
		case respOptKeepTTL:
			opts.keepTTL = true
		default:
			if err := opts.setExpiry(command, name, args[i+1:]); err != nil {
				return opts, err
			}
			i++
		}
	}

	if (opts.nx && opts.xx) || (opts.keepTTL && opts.expiry != "") {
		return opts, errRESPSyntax
	}

	return opts, nil
}

func (o *respSetOptions) setExpiry(command, unit string, rest [][]byte) error {
	if _, ok := respExpiryAt(unit, 0, time.Time{}); !ok {
		return errRESPSyntax
	}
	if len(rest) == 0 || o.expiry != "" {
		return errRESPSyntax
	}

	amount, err := strconv.ParseInt(string(rest[0]), 10, 64)
	if err != nil {
		return errRESPNotInteger
	}
	if amount <= 0 && (unit == respUnitEX || unit == respUnitPX) {
		return errRESPExpireTime(command)
	}
	if !respExpiryFits(unit, amount) {
		return errRESPExpireTime(command)
	}

	o.expiry, o.expiryArg = unit, amount

	return nil
}

// expiresAt resolves the parsed expiry against now.
func (o respSetOptions) expiresAt(now time.Time) (time.Time, bool) {
	if o.expiry == "" {
		return time.Time{}, false
	}

	return respExpiryAt(o.expiry, o.expiryArg, now)
}

func (c *respConn) cmdGet(args [][]byte) error {
	var value []byte

	err := c.read(func(tx *kvs.ReadTx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		stored, err := respStringOf(entry)
		if err != nil {
			return err
		}
		value = []byte(stored)

		return nil
	})
	if err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(value)
}

func (c *respConn) cmdSet(args [][]byte) error {
	opts, err := parseSetOptions(string(args[0]), args[3:])
	if err != nil {
		return c.writeFailure(err)
	}

	key := string(args[1])
	var previous []byte
	var stored bool

	err = c.write(func(tx *kvs.Tx) error {
		entry, exists := tx.Get(key)
		if exists && opts.get {
			old, typeErr := respStringOf(entry)
			if typeErr != nil {
				return typeErr
			}
			previous = []byte(old)
		}
		if (opts.nx && exists) || (opts.xx && !exists) {
			return nil
		}

		next := kvs.Entry{Value: string(args[2])}
		if opts.keepTTL && exists {
			next.ExpiresAt = entry.ExpiresAt
		}
		if at, ok := opts.expiresAt(tx.Now()); ok {
			next.ExpiresAt = at
		}
		tx.Set(key, next)
		stored = true

		return nil
	})
	if err != nil {
		return c.writeFailure(err)
	}

	if opts.get {
		return c.writer.WriteBulk(previous)
	}
	if !stored {
		return c.writer.WriteNull()
	}

	return c.writer.WriteSimple(respOK)
}

// cmdSetNX is the standalone form of SET with the NX option.
func (c *respConn) cmdSetNX(args [][]byte) error {
	var stored bool

	if err := c.write(func(tx *kvs.Tx) error {
		if _, exists := tx.Get(string(args[1])); exists {
			return nil
		}
		tx.Set(string(args[1]), kvs.Entry{Value: string(args[2])})
		stored = true

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(boolToInt(stored))
}

// cmdSetEX handles both SETEX, whose expiry is in seconds, and PSETEX in milliseconds.
func (c *respConn) cmdSetEX(args [][]byte) error {
	unit := respUnitEX
	if strings.EqualFold(string(args[0]), respCmdPSetEX) {
		unit = respUnitPX
	}

	amount, err := strconv.ParseInt(string(args[2]), 10, 64)
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}
	if amount <= 0 || !respExpiryFits(unit, amount) {
		return c.writeFailure(errRESPExpireTime(string(args[0])))
	}

	if err := c.write(func(tx *kvs.Tx) error {
		at, _ := respExpiryAt(unit, amount, tx.Now())
		tx.Set(string(args[1]), kvs.Entry{Value: string(args[3]), ExpiresAt: at})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteSimple(respOK)
}

func (c *respConn) cmdGetSet(args [][]byte) error {
	var previous []byte

	if err := c.write(func(tx *kvs.Tx) error {
		if entry, ok := tx.Get(string(args[1])); ok {
			old, err := respStringOf(entry)
			if err != nil {
				return err
			}
			previous = []byte(old)
		}
		tx.Set(string(args[1]), kvs.Entry{Value: string(args[2])})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(previous)
}

func (c *respConn) cmdGetDel(args [][]byte) error {
	var previous []byte

	if err := c.write(func(tx *kvs.Tx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		old, err := respStringOf(entry)
		if err != nil {
			return err
		}
		previous = []byte(old)
		tx.Delete(string(args[1]))

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulk(previous)
}

func (c *respConn) cmdMGet(args [][]byte) error {
	values := make([][]byte, 0, len(args)-1)

	if err := c.read(func(tx *kvs.ReadTx) error {
		for _, key := range args[1:] {
			entry, ok := tx.Get(string(key))
			if !ok {
				values = append(values, nil)

				continue
			}
			// Unlike GET, a key of the wrong type is reported as absent rather than as an
			// error, so that one odd key does not fail the whole lookup.
			stored, err := respStringOf(entry)
			if err != nil {
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

// cmdMSet handles MSET and MSETNX, which differ only in whether an existing key makes the
// whole batch a no-op.
func (c *respConn) cmdMSet(args [][]byte) error {
	pairs := args[1:]
	if len(pairs)%2 != 0 {
		return c.wrongArgs(string(args[0]))
	}

	onlyIfAbsent := strings.EqualFold(string(args[0]), respCmdMSetNX)
	stored := true

	if err := c.write(func(tx *kvs.Tx) error {
		if onlyIfAbsent {
			for i := 0; i < len(pairs); i += 2 {
				if _, exists := tx.Get(string(pairs[i])); exists {
					stored = false

					return nil
				}
			}
		}
		for i := 0; i < len(pairs); i += 2 {
			tx.Set(string(pairs[i]), kvs.Entry{Value: string(pairs[i+1])})
		}

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	if onlyIfAbsent {
		return c.writer.WriteInt(boolToInt(stored))
	}

	return c.writer.WriteSimple(respOK)
}

// cmdIncr handles INCR, DECR, INCRBY, and DECRBY.
func (c *respConn) cmdIncr(args [][]byte) error {
	delta := int64(1)
	if len(args) == 3 {
		parsed, err := strconv.ParseInt(string(args[2]), 10, 64)
		if err != nil {
			return c.writer.WriteError(respErrNotInteger)
		}
		delta = parsed
	}
	if name := strings.ToUpper(string(args[0])); name == respCmdDecr || name == respCmdDecrBy {
		if delta == math.MinInt64 {
			return c.writer.WriteError(respErrDecrOver)
		}
		delta = -delta
	}

	var result int64
	if err := c.write(func(tx *kvs.Tx) error {
		current := int64(0)
		expiresAt := time.Time{}

		if entry, ok := tx.Get(string(args[1])); ok {
			stored, err := respStringOf(entry)
			if err != nil {
				return err
			}
			if current, err = strconv.ParseInt(stored, 10, 64); err != nil {
				return errRESPNotInteger
			}
			expiresAt = entry.ExpiresAt
		}

		if addOverflows(current, delta) {
			return errRESPOverflow
		}
		result = current + delta
		// An increment keeps the key's expiry, as Redis does.
		tx.Set(string(args[1]), kvs.Entry{Value: strconv.FormatInt(result, 10), ExpiresAt: expiresAt})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(result)
}

func (c *respConn) cmdIncrByFloat(args [][]byte) error {
	delta, err := strconv.ParseFloat(string(args[2]), 64)
	if err != nil {
		return c.writer.WriteError(errRESPNotFloat.Error())
	}

	var result string
	if err := c.write(func(tx *kvs.Tx) error {
		current := float64(0)
		expiresAt := time.Time{}

		if entry, ok := tx.Get(string(args[1])); ok {
			stored, err := respStringOf(entry)
			if err != nil {
				return err
			}
			if current, err = strconv.ParseFloat(stored, 64); err != nil {
				return errRESPNotFloat
			}
			expiresAt = entry.ExpiresAt
		}

		result = respFormatFloat(current + delta)
		tx.Set(string(args[1]), kvs.Entry{Value: result, ExpiresAt: expiresAt})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteBulkString(result)
}

func (c *respConn) cmdAppend(args [][]byte) error {
	var length int

	if err := c.write(func(tx *kvs.Tx) error {
		current := ""
		expiresAt := time.Time{}

		if entry, ok := tx.Get(string(args[1])); ok {
			stored, err := respStringOf(entry)
			if err != nil {
				return err
			}
			current, expiresAt = stored, entry.ExpiresAt
		}

		if len(current) > resp.MaxBulkLength-len(args[2]) {
			return errRESPStringTooLong
		}

		next := current + string(args[2])
		length = len(next)
		tx.Set(string(args[1]), kvs.Entry{Value: next, ExpiresAt: expiresAt})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}

func (c *respConn) cmdStrLen(args [][]byte) error {
	var length int

	if err := c.read(func(tx *kvs.ReadTx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		stored, err := respStringOf(entry)
		if err != nil {
			return err
		}
		length = len(stored)

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}

func (c *respConn) cmdGetRange(args [][]byte) error {
	start, err := strconv.Atoi(string(args[2]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}
	end, err := strconv.Atoi(string(args[3]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}

	var value string
	if err := c.read(func(tx *kvs.ReadTx) error {
		entry, ok := tx.Get(string(args[1]))
		if !ok {
			return nil
		}

		stored, err := respStringOf(entry)
		if err != nil {
			return err
		}
		value = stored

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	from, to, ok := respRange(start, end, len(value))
	if !ok {
		return c.writer.WriteBulkString("")
	}

	return c.writer.WriteBulkString(value[from:to])
}

func (c *respConn) cmdSetRange(args [][]byte) error {
	offset, err := strconv.Atoi(string(args[2]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}
	if offset < 0 {
		return c.writer.WriteError("ERR offset is out of range")
	}
	// Written as a subtraction because offset+len(patch) overflows for an offset near MaxInt,
	// which used to wrap negative, skip the zero padding, and panic on the copy below.
	if offset > resp.MaxBulkLength-len(args[3]) {
		return c.writer.WriteError(respErrStringTooLong)
	}

	var length int
	if err := c.write(func(tx *kvs.Tx) error {
		current := ""
		expiresAt := time.Time{}

		if entry, ok := tx.Get(string(args[1])); ok {
			stored, err := respStringOf(entry)
			if err != nil {
				return err
			}
			current, expiresAt = stored, entry.ExpiresAt
		}

		patch := string(args[3])
		if patch == "" {
			length = len(current)

			return nil
		}

		// A write past the end zero-pads the gap, as Redis does.
		next := []byte(current)
		if grow := offset + len(patch) - len(next); grow > 0 {
			next = append(next, make([]byte, grow)...)
		}
		copy(next[offset:], patch)
		length = len(next)
		tx.Set(string(args[1]), kvs.Entry{Value: string(next), ExpiresAt: expiresAt})

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	return c.writer.WriteInt(int64(length))
}
