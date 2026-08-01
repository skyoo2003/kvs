package resp

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

const crlf = "\r\n"

// Writer writes RESP2 replies. Replies are buffered, so Flush must be called before the
// server waits on the client again.
type Writer struct {
	bw *bufio.Writer
}

// NewWriter wraps w with reply buffering.
func NewWriter(w io.Writer) *Writer {
	return &Writer{bw: bufio.NewWriter(w)}
}

// WriteSimple writes a simple string reply such as +OK.
func (w *Writer) WriteSimple(s string) error {
	return w.writeLine('+', s)
}

// WriteError writes an error reply. By convention the message starts with an uppercase
// code, as in "ERR unknown command".
func (w *Writer) WriteError(s string) error {
	return w.writeLine('-', s)
}

// WriteInt writes an integer reply.
func (w *Writer) WriteInt(n int64) error {
	return w.writeLine(':', strconv.FormatInt(n, 10))
}

// WriteNull writes the RESP2 null bulk string, the reply for a missing key.
func (w *Writer) WriteNull() error {
	return w.writeLine('$', "-1")
}

// WriteNullArray writes the RESP2 null array, the reply for an aborted transaction.
func (w *Writer) WriteNullArray() error {
	return w.writeLine('*', "-1")
}

// WriteBulk writes a bulk string reply. A nil slice writes a null bulk string, so that a
// missing value and an empty value stay distinguishable.
func (w *Writer) WriteBulk(b []byte) error {
	if b == nil {
		return w.WriteNull()
	}

	if err := w.writeLine('$', strconv.Itoa(len(b))); err != nil {
		return err
	}
	if _, err := w.bw.Write(b); err != nil {
		return err
	}

	_, err := w.bw.WriteString(crlf)

	return err
}

// WriteBulkString writes s as a bulk string reply. Unlike WriteBulk an empty s is still
// an empty bulk string rather than a null.
func (w *Writer) WriteBulkString(s string) error {
	if err := w.writeLine('$', strconv.Itoa(len(s))); err != nil {
		return err
	}
	if _, err := w.bw.WriteString(s); err != nil {
		return err
	}

	_, err := w.bw.WriteString(crlf)

	return err
}

// WriteArrayHeader announces an array reply of n elements, which the caller then writes
// one by one.
func (w *Writer) WriteArrayHeader(n int) error {
	return w.writeLine('*', strconv.Itoa(n))
}

// WriteStrings writes items as an array of bulk strings.
func (w *Writer) WriteStrings(items []string) error {
	if err := w.WriteArrayHeader(len(items)); err != nil {
		return err
	}

	for _, item := range items {
		if err := w.WriteBulkString(item); err != nil {
			return err
		}
	}

	return nil
}

// WriteRaw writes bytes that are already in RESP form, which is how a caller that encoded a
// reply elsewhere splices it into the stream.
func (w *Writer) WriteRaw(encoded []byte) error {
	_, err := w.bw.Write(encoded)

	return err
}

// Flush sends every buffered reply.
func (w *Writer) Flush() error {
	return w.bw.Flush()
}

// writeLine writes one prefixed line, stripping CR and LF from s. Untrusted text such as
// a client-supplied command name reaches error replies, and without this a value
// containing CRLF would inject an extra protocol frame into the reply stream.
func (w *Writer) writeLine(prefix byte, s string) error {
	if err := w.bw.WriteByte(prefix); err != nil {
		return err
	}
	if _, err := w.bw.WriteString(stripNewlines(s)); err != nil {
		return err
	}

	_, err := w.bw.WriteString(crlf)

	return err
}

func stripNewlines(s string) string {
	if !strings.ContainsAny(s, crlf) {
		return s
	}

	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}

		return r
	}, s)
}
