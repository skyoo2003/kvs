// Package resp implements the RESP2 wire protocol spoken by Redis and Valkey clients.
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
