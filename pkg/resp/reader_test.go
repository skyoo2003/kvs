package resp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestReaderReadCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  [][]byte
	}{
		{
			name:  "array form",
			input: "*2\r\n$4\r\nECHO\r\n$2\r\nhi\r\n",
			want:  [][]byte{[]byte("ECHO"), []byte("hi")},
		},
		{
			name:  "inline form",
			input: "PING\r\n",
			want:  [][]byte{[]byte("PING")},
		},
		{
			name:  "inline form with arguments",
			input: "SET  foo\tbar\r\n",
			want:  [][]byte{[]byte("SET"), []byte("foo"), []byte("bar")},
		},
		{
			// telnet and nc send a bare LF, which is the whole point of the inline form.
			name:  "inline form terminated by a bare line feed",
			input: "PING\n",
			want:  [][]byte{[]byte("PING")},
		},
		{
			name:  "empty bulk string is not a null",
			input: "*1\r\n$0\r\n\r\n",
			want:  [][]byte{{}},
		},
		{
			name:  "bulk string carries a crlf payload",
			input: "*1\r\n$4\r\na\r\nb\r\n",
			want:  [][]byte{[]byte("a\r\nb")},
		},
		{
			name:  "bulk string carries a nul byte",
			input: "*1\r\n$3\r\na\x00b\r\n",
			want:  [][]byte{[]byte("a\x00b")},
		},
		{
			name:  "blank line is skipped",
			input: "\r\n",
			want:  nil,
		},
		{
			name:  "empty array is skipped",
			input: "*0\r\n",
			want:  nil,
		},
		{
			name:  "null array is skipped",
			input: "*-1\r\n",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewReader(strings.NewReader(tt.input)).ReadCommand()
			if err != nil {
				t.Fatalf("ReadCommand() error = %v", err)
			}
			if !slices.EqualFunc(got, tt.want, bytes.Equal) {
				t.Fatalf("ReadCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReaderRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "non numeric array length", input: "*abc\r\n"},
		{name: "argument is not a bulk string", input: "*1\r\n+OK\r\n"},
		{name: "non numeric bulk length", input: "*1\r\n$xx\r\nab\r\n"},
		{name: "negative bulk length", input: "*1\r\n$-1\r\n"},
		{name: "bulk length above the cap", input: "*1\r\n$536870913\r\n"},
		{name: "array length above the cap", input: "*1048577\r\n"},
		{name: "bulk string not terminated by crlf", input: "*1\r\n$1\r\nabc\r\n"},
		{name: "inline request longer than the buffer", input: strings.Repeat("A", readerBufferSize+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tt.input)).ReadCommand()
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("ReadCommand() error = %v, want %v", err, ErrProtocol)
			}
		})
	}
}

func TestReaderReportsEOFOnClosedStream(t *testing.T) {
	_, err := NewReader(strings.NewReader("")).ReadCommand()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadCommand() error = %v, want %v", err, io.EOF)
	}
}

func TestReaderReadsPipelinedCommands(t *testing.T) {
	reader := NewReader(strings.NewReader("*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\nPING\r\n"))

	for i := range 3 {
		got, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("ReadCommand(%d) error = %v", i, err)
		}
		if len(got) != 1 || string(got[0]) != "PING" {
			t.Fatalf("ReadCommand(%d) = %q, want [PING]", i, got)
		}
	}

	if _, err := reader.ReadCommand(); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadCommand() after batch error = %v, want %v", err, io.EOF)
	}
}

// TestReaderDoesNotPreallocateDeclaredLength checks that a bulk string header alone cannot
// make the reader reserve memory: the client announces the maximum and sends nothing, so the
// read has to fail on the missing payload rather than on an allocation.
func TestReaderDoesNotPreallocateDeclaredLength(t *testing.T) {
	before := heapInUse()

	_, err := NewReader(strings.NewReader("*1\r\n$536870912\r\n")).ReadCommand()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadCommand() error = %v, want %v", err, io.ErrUnexpectedEOF)
	}

	// Compare as unsigned and only in the growth direction; the heap may also have shrunk.
	if after := heapInUse(); after > before && after-before > 8*1024*1024 {
		t.Fatalf("heap grew by %d bytes for an unsent 512MB payload, want it to track the data", after-before)
	}
}

func TestParseReply(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{name: "status", input: "+OK\r\n", want: Status("OK")},
		{name: "error", input: "-ERR nope\r\n", want: Error("ERR nope")},
		{name: "integer", input: ":42\r\n", want: int64(42)},
		{name: "negative integer", input: ":-1\r\n", want: int64(-1)},
		{name: "bulk string", input: "$2\r\nhi\r\n", want: []byte("hi")},
		{name: "empty bulk string", input: "$0\r\n\r\n", want: []byte("")},
		{name: "null bulk string", input: "$-1\r\n", want: []byte(nil)},
		{name: "bulk string holding a terminator", input: "$3\r\na\r\n\r\n", want: []byte("a\r\n")},
		{name: "array", input: "*2\r\n:1\r\n$2\r\nhi\r\n", want: []any{int64(1), []byte("hi")}},
		{name: "empty array", input: "*0\r\n", want: []any{}},
		{name: "null array", input: "*-1\r\n", want: []any(nil)},
		{name: "nested array", input: "*1\r\n*1\r\n:1\r\n", want: []any{[]any{int64(1)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rest, err := ParseReply([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseReply(%q) error = %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseReply(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			if len(rest) != 0 {
				t.Fatalf("ParseReply(%q) rest = %q, want it consumed", tt.input, rest)
			}
		})
	}
}

// TestParseReplyLeavesTheRest covers reading a batch, which is how a caller walks the replies
// a pipeline or a transaction produced.
func TestParseReplyLeavesTheRest(t *testing.T) {
	first, rest, err := ParseReply([]byte(":1\r\n:2\r\n"))
	if err != nil || first != int64(1) {
		t.Fatalf("ParseReply() = %v, %v, want 1", first, err)
	}

	second, rest, err := ParseReply(rest)
	if err != nil || second != int64(2) {
		t.Fatalf("ParseReply() = %v, %v, want 2", second, err)
	}
	if len(rest) != 0 {
		t.Fatalf("rest = %q, want it consumed", rest)
	}
}

func TestParseReplyRejectsMalformedInput(t *testing.T) {
	tests := map[string]string{
		"nothing":              "",
		"no terminator":        "+OK",
		"unknown type":         "!boom\r\n",
		"a bad integer":        ":nope\r\n",
		"a bad bulk length":    "$x\r\n",
		"a truncated bulk":     "$5\r\nhi\r\n",
		"an unterminated bulk": "$2\r\nhi!!\r\n",
		"a bad array length":   "*x\r\n",
		"a short array":        "*2\r\n:1\r\n",
		// The depth bound only matters for a stream from somewhere else, which is exactly
		// where an adversarial one would come from.
		"too deeply nested": strings.Repeat("*1\r\n", replyMaxDepth+2),
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseReply([]byte(input)); !errors.Is(err, ErrProtocol) {
				t.Fatalf("ParseReply(%q) error = %v, want %v", input, err, ErrProtocol)
			}
		})
	}
}

func heapInUse() uint64 {
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return stats.HeapInuse
}
