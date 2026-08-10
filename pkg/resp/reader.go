// Package resp implements the RESP2 wire protocol spoken by Redis and Valkey clients.
//
// It exists for the server in this module to speak that protocol, not as a RESP library for
// other programs, and is outside the v1 compatibility promise: see
// content/docs/compatibility.md. The protocol kvs answers on the wire is promised; this Go
// package is not.
package resp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	// MaxBulkLength caps a single bulk string, matching the Redis proto-max-bulk-len default.
	// The buffer for a bulk string grows as the payload arrives, so a declared length costs
	// nothing until the bytes behind it do.
	MaxBulkLength = 512 * 1024 * 1024

	// MaxArrayLength caps how many arguments a single command may carry.
	MaxArrayLength = 1024 * 1024

	// readerBufferSize bounds inline commands and type headers. Bulk payloads are read
	// straight into their own buffer, so they are not limited by this.
	readerBufferSize = 64 * 1024

	// argPrealloc bounds the argument slice reserved up front, so that a small request
	// claiming a huge argument count cannot make the server allocate ahead of the data.
	argPrealloc = 64

	// bulkPrealloc bounds the buffer reserved for a bulk string before its payload arrives.
	// Anything larger grows as the bytes come in, so announcing a 512MB value and then
	// stalling costs the server nothing.
	bulkPrealloc = 64 * 1024
)

// ErrProtocol reports a malformed request. A server should report it to the client and
// then close the connection, because the stream can no longer be resynchronized.
var ErrProtocol = errors.New("protocol error")

// Reader reads client commands from a stream.
type Reader struct {
	br *bufio.Reader
}

// NewReader wraps r with the buffering that ReadCommand needs.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, readerBufferSize)}
}

// ReadCommand reads the next command as its raw argument list. It accepts both the array
// form that clients send and the inline form typed by hand over telnet. A blank line or
// an empty array yields a nil slice with no error, which callers should skip.
func (r *Reader) ReadCommand() ([][]byte, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}

	if len(line) == 0 || line[0] != '*' {
		return splitInline(line), nil
	}

	count, err := parseCount(line[1:])
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, nil
	}
	if count > MaxArrayLength {
		return nil, fmt.Errorf("%w: invalid multibulk length", ErrProtocol)
	}

	args := make([][]byte, 0, min(count, argPrealloc))
	for range count {
		var arg []byte
		arg, err = r.readBulk()
		if err != nil {
			return nil, err
		}

		args = append(args, arg)
	}

	return args, nil
}

// readLine returns the next line without its terminator. The result points into the read
// buffer and stays valid only until the next read.
//
// Clients terminate every line with CRLF, but a bare LF is accepted too: that is what a
// hand-typed inline command over telnet or nc sends, and Redis tolerates it for the same
// reason. Bulk payloads are still framed strictly, since nothing types those by hand.
func (r *Reader) readLine() ([]byte, error) {
	line, err := r.br.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("%w: too big inline request", ErrProtocol)
		}

		return nil, err
	}

	line = line[:len(line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line, nil
}

func (r *Reader) readBulk() ([]byte, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '$' {
		return nil, fmt.Errorf("%w: expected '$', got a different type", ErrProtocol)
	}

	length, err := parseCount(line[1:])
	if err != nil {
		return nil, err
	}
	if length < 0 || length > MaxBulkLength {
		return nil, fmt.Errorf("%w: invalid bulk length", ErrProtocol)
	}

	// Read the payload and its terminator into a buffer that grows with the data instead of
	// reserving the announced length, which is what keeps a declared size from being a
	// cheap way to make the server allocate.
	wanted := length + len(crlf)
	buf := bytes.NewBuffer(make([]byte, 0, min(wanted, bulkPrealloc)))
	if _, err = io.CopyN(buf, r.br, int64(wanted)); err != nil {
		// A short copy means the announced payload never fully arrived, which is a truncated
		// command rather than a clean end of stream.
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}

		return nil, err
	}

	payload := buf.Bytes()
	if string(payload[length:]) != crlf {
		return nil, fmt.Errorf("%w: unterminated bulk string", ErrProtocol)
	}

	return payload[:length], nil
}

// Status is a simple string reply such as +OK. It is a type of its own so that a caller
// converting replies back into values can tell one from a bulk string carrying the same bytes.
type Status string

// Error is an error reply, reported as a value rather than a Go error: whether a failed
// command is the caller's own failure is the caller's decision, not the parser's.
type Error string

// replyMaxDepth bounds how deeply arrays may nest. Replies kvs writes nest two levels at most,
// so it only matters elsewhere, where a stream of "*1" lines would recurse until the stack goes.
const replyMaxDepth = 32

// ParseReply reads one reply off the front of encoded and returns it with whatever follows, so
// a caller holding a batch can keep going. The value is one of Status, Error, int64, []byte for
// a bulk string, or []any for an array; a null of either parses to a nil slice of its own type,
// which keeps a missing value distinguishable from an empty one.
func ParseReply(encoded []byte) (value any, rest []byte, err error) {
	return parseReply(encoded, 0)
}

func parseReply(encoded []byte, depth int) (value any, rest []byte, err error) {
	if depth > replyMaxDepth {
		return nil, nil, fmt.Errorf("%w: reply nested too deeply", ErrProtocol)
	}

	line, rest, err := cutLine(encoded)
	if err != nil {
		return nil, nil, err
	}
	if len(line) == 0 {
		return nil, nil, fmt.Errorf("%w: empty reply", ErrProtocol)
	}

	body := line[1:]
	switch line[0] {
	case '+':
		return Status(body), rest, nil
	case '-':
		return Error(body), rest, nil
	case ':':
		number, convErr := strconv.ParseInt(string(body), 10, 64)
		if convErr != nil {
			return nil, nil, fmt.Errorf("%w: invalid integer %q", ErrProtocol, body)
		}

		return number, rest, nil
	case '$':
		return parseBulkReply(body, rest)
	case '*':
		return parseArrayReply(body, rest, depth)
	default:
		return nil, nil, fmt.Errorf("%w: unknown reply type %q", ErrProtocol, line[0])
	}
}

func parseBulkReply(header, rest []byte) (value any, remaining []byte, err error) {
	length, err := parseCount(header)
	if err != nil {
		return nil, nil, err
	}
	if length < 0 {
		// A nil slice rather than an empty one: only this branch means the value was not there.
		return []byte(nil), rest, nil
	}
	// Checking the announced length before adding keeps the sum below in range.
	if length > MaxBulkLength || length+len(crlf) > len(rest) {
		return nil, nil, fmt.Errorf("%w: truncated bulk string", ErrProtocol)
	}
	if string(rest[length:length+len(crlf)]) != crlf {
		return nil, nil, fmt.Errorf("%w: unterminated bulk string", ErrProtocol)
	}

	return rest[:length], rest[length+len(crlf):], nil
}

func parseArrayReply(header, rest []byte, depth int) (value any, remaining []byte, err error) {
	count, err := parseCount(header)
	if err != nil {
		return nil, nil, err
	}
	if count < 0 {
		return []any(nil), rest, nil
	}
	if count > MaxArrayLength {
		return nil, nil, fmt.Errorf("%w: invalid multibulk length", ErrProtocol)
	}

	items := make([]any, 0, min(count, argPrealloc))
	for range count {
		var item any
		if item, rest, err = parseReply(rest, depth+1); err != nil {
			return nil, nil, err
		}

		items = append(items, item)
	}

	return items, rest, nil
}

// cutLine splits off the next CRLF terminated line, without its terminator.
func cutLine(encoded []byte) (line, rest []byte, err error) {
	at := bytes.Index(encoded, []byte(crlf))
	if at < 0 {
		return nil, nil, fmt.Errorf("%w: unterminated reply line", ErrProtocol)
	}

	return encoded[:at], encoded[at+len(crlf):], nil
}

func parseCount(b []byte) (int, error) {
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return 0, fmt.Errorf("%w: invalid length %q", ErrProtocol, b)
	}

	return n, nil
}

// splitInline splits a whitespace-separated inline command. Quoting is not supported;
// inline commands exist for hand debugging, and real clients use the array form.
func splitInline(line []byte) [][]byte {
	fields := bytes.Fields(line)
	if len(fields) == 0 {
		return nil
	}

	args := make([][]byte, 0, len(fields))
	for _, field := range fields {
		args = append(args, bytes.Clone(field))
	}

	return args
}
