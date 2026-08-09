package kvs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/skyoo2003/kvs/internal/datadir"
)

// ErrUnsupportedValue is what a Codec returns for a value it cannot encode. Failing the write
// is deliberate: a value quietly skipped here is a value gone at the next restart.
var ErrUnsupportedValue = errors.New("unsupported value type")

// Codec turns the values a Store holds into bytes and back, so the log can persist a keyspace
// whose value types it does not know. A caller storing nothing but strings can use
// StringCodec; one storing its own types supplies its own.
type Codec interface {
	Encode(value interface{}) ([]byte, error)
	Decode(data []byte) (interface{}, error)
	// Clone copies a value far enough that the copy and the original can be changed
	// independently. Speculate hands callers a clone, which is what lets it put the keyspace
	// back afterwards: a command that changes a container in place then changes only the copy.
	Clone(value interface{}) interface{}
}

// StringCodec persists string values.
type StringCodec struct{}

func (StringCodec) Encode(value interface{}) ([]byte, error) {
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedValue, value)
	}

	return []byte(text), nil
}

func (StringCodec) Decode(data []byte) (interface{}, error) {
	return string(data), nil
}

// Clone returns the value unchanged: strings cannot be changed in place.
func (StringCodec) Clone(value interface{}) interface{} {
	return value
}

// The operations a record can carry. Between them they cover every change a transaction can
// make to the keyspace.
const (
	opSet   = "set"
	opDel   = "del"
	opFlush = "flush"
)

// logName is the file the append log lives in, inside the directory Open was given. The name
// belongs to datadir because that package has to recognize a directory holding one, and two
// spellings of it would mean a log this build writes and the next one fails to notice.
const logName = datadir.LogName

// record is one durable change. Value holds the encoded form and value the original: encoding
// is deferred to commit so that a value the codec cannot handle fails the write that stored
// it, rather than vanishing between here and the next startup.
type record struct {
	Op        string    `json:"op"`
	Key       string    `json:"key,omitempty"`
	Value     []byte    `json:"value,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`

	value interface{}
}

// frame is one transaction's worth of encoded records. Keeping a transaction's records
// together is what lets a replica apply a MULTI the way the leader ran it: all of it or none.
type frame [][]byte

// encodeRecord renders one record as the line the log stores and the wire carries. The log and
// replication share it, so a replica applies the same bytes the leader wrote down.
func encodeRecord(rec *record) ([]byte, error) {
	line, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("encode record %q: %w", rec.Key, err)
	}

	return line, nil
}

func decodeRecord(line []byte) (record, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return rec, fmt.Errorf("decode record: %w", err)
	}

	return rec, nil
}

// appendLog is the on-disk change log. Every committed write appends to it, and a Store built
// by Open replays it before serving anything.
type appendLog struct {
	file *os.File
	path string
}

// openLog opens, creating if needed, the log inside dir. The file is positioned for reading:
// rewrite reopens it for appending once the replay is done.
func openLog(dir string) (*appendLog, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dir, logName)
	//nolint:gosec // The path is the data directory the operator named; that is the feature.
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}

	return &appendLog{file: file, path: path}, nil
}

// replay feeds every record to apply, in the order they were written.
//
// A log whose last record was cut short stops there. That is what a crash part-way through an
// append leaves behind, and the records before it are still sound, so the reader keeps them,
// says how much it dropped, and lets the startup rewrite clear the remains.
func (l *appendLog) replay(apply func(rec *record) error) error {
	decoder := json.NewDecoder(l.file)

	for {
		var rec record
		err := decoder.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			size := decoder.InputOffset()
			if info, statErr := l.file.Stat(); statErr == nil {
				size = info.Size()
			}
			log.Printf("kvs: %s is unreadable %d bytes in, dropping the remaining %d: %v",
				l.path, decoder.InputOffset(), size-decoder.InputOffset(), err)

			return nil
		}

		if err := apply(&rec); err != nil {
			return err
		}
	}
}

// append writes the records and flushes them before returning, so a write the caller has been
// told succeeded has actually reached the disk.
//
// ponytail: the flush happens under the store's write lock, which caps writes at the disk's
// sync rate. Group commit, or a flush window of a second the way Redis does it, is the upgrade
// path if that ceiling ever costs more than the guarantee is worth.
func (l *appendLog) append(lines frame) error {
	return writeLines(l.file, lines)
}

// rewrite replaces the log with one holding just the records given and leaves it open for
// appending. Startup does this right after the replay, when the whole live keyspace is already
// in memory: compaction therefore costs nothing extra and needs no background worker.
//
// ponytail: the log only shrinks at startup, so a long-running process grows it without bound.
// Rewriting once it outgrows the keyspace by some factor is the upgrade path.
func (l *appendLog) rewrite(lines frame) error {
	dir := filepath.Dir(l.path)

	temp, err := os.CreateTemp(dir, logName+".*")
	if err != nil {
		return fmt.Errorf("create log: %w", err)
	}
	// Harmless once the rename below has moved the file out from under it, and the only way
	// a failure part-way through does not leave litter behind.
	defer func() { _ = os.Remove(temp.Name()) }()

	if err = writeLines(temp, lines); err != nil {
		_ = temp.Close()

		return err
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("close log: %w", err)
	}

	if err = os.Rename(temp.Name(), l.path); err != nil {
		return fmt.Errorf("replace log: %w", err)
	}
	// The rename itself has to reach the disk, or a crash can leave the directory naming
	// neither the old file nor the new one.
	if syncErr := syncDir(dir); syncErr != nil {
		return syncErr
	}

	if err = l.file.Close(); err != nil {
		return fmt.Errorf("close log: %w", err)
	}

	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("reopen log: %w", err)
	}
	l.file = file

	return nil
}

func (l *appendLog) close() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close log: %w", err)
	}

	return nil
}

// writeLines appends the frame as one newline separated record per line, the format replay
// reads back, and flushes it before returning.
func writeLines(file *os.File, lines frame) error {
	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.Write(line); err != nil {
			return fmt.Errorf("write log: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write log: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write log: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("flush log: %w", err)
	}

	return nil
}

func syncDir(dir string) error {
	//nolint:gosec // The path is the data directory the operator named; that is the feature.
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open data dir: %w", err)
	}
	defer func() { _ = handle.Close() }()

	if err := handle.Sync(); err != nil {
		return fmt.Errorf("flush data dir: %w", err)
	}

	return nil
}
