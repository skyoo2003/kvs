package resp

import (
	"bytes"
	"testing"
)

func TestWriterRepliesMatchTheWireFormat(t *testing.T) {
	tests := []struct {
		name  string
		write func(w *Writer) error
		want  string
	}{
		{
			name:  "simple string",
			write: func(w *Writer) error { return w.WriteSimple("OK") },
			want:  "+OK\r\n",
		},
		{
			name:  "error",
			write: func(w *Writer) error { return w.WriteError("ERR unknown command") },
			want:  "-ERR unknown command\r\n",
		},
		{
			name:  "integer",
			write: func(w *Writer) error { return w.WriteInt(-42) },
			want:  ":-42\r\n",
		},
		{
			name:  "bulk string",
			write: func(w *Writer) error { return w.WriteBulk([]byte("hello")) },
			want:  "$5\r\nhello\r\n",
		},
		{
			name:  "bulk string keeps a crlf payload intact",
			write: func(w *Writer) error { return w.WriteBulk([]byte("a\r\nb")) },
			want:  "$4\r\na\r\nb\r\n",
		},
		{
			name:  "nil bulk is a null",
			write: func(w *Writer) error { return w.WriteBulk(nil) },
			want:  "$-1\r\n",
		},
		{
			name:  "empty string bulk is not a null",
			write: func(w *Writer) error { return w.WriteBulkString("") },
			want:  "$0\r\n\r\n",
		},
		{
			name:  "null",
			write: func(w *Writer) error { return w.WriteNull() },
			want:  "$-1\r\n",
		},
		{
			name:  "null array",
			write: func(w *Writer) error { return w.WriteNullArray() },
			want:  "*-1\r\n",
		},
		{
			name:  "array of bulk strings",
			write: func(w *Writer) error { return w.WriteStrings([]string{"a", "bc"}) },
			want:  "*2\r\n$1\r\na\r\n$2\r\nbc\r\n",
		},
		{
			name:  "empty array",
			write: func(w *Writer) error { return w.WriteStrings(nil) },
			want:  "*0\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := NewWriter(&buf)

			if err := tt.write(writer); err != nil {
				t.Fatalf("write error = %v", err)
			}
			if err := writer.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			if buf.String() != tt.want {
				t.Fatalf("reply = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestWriterStripsNewlinesFromLineReplies(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)

	// A command name echoed back into an error reply is client-controlled, so a payload
	// carrying CRLF must not be able to append a frame of its own.
	if err := writer.WriteError("ERR unknown command 'evil\r\n+INJECTED'"); err != nil {
		t.Fatalf("WriteError() error = %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	const want = "-ERR unknown command 'evil  +INJECTED'\r\n"
	if buf.String() != want {
		t.Fatalf("reply = %q, want %q", buf.String(), want)
	}
}

func TestWriterBuffersUntilFlush(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)

	if err := writer.WriteSimple("PONG"); err != nil {
		t.Fatalf("WriteSimple() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buffer = %q, want the reply to be held until Flush", buf.String())
	}

	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if buf.String() != "+PONG\r\n" {
		t.Fatalf("reply = %q, want %q", buf.String(), "+PONG\r\n")
	}
}
