// Package datadir owns the shape of the directory kvs keeps its data in, and the version
// stamped on it. Every path that opens one checks that version before reading anything, so a
// directory this build cannot understand fails at startup with a reason rather than part-way
// through a replay, or worse, quietly.
package datadir

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version is what this build writes and the only one it reads. Raise it whenever the bytes in
// a data directory change shape — including when a dependency that owns part of the directory,
// the Raft log store among them, changes its own format under us. There is no conversion
// between versions, which is why the check has to be loud.
const Version = 1

const (
	// FormatName is the file holding Version. It sits beside the data rather than inside it
	// because the Raft store's files belong to a library that will not carry our header.
	FormatName = "format"

	// LogName is the single-node append log, and RaftName the directory the Raft store keeps
	// its own files in. Together they are how an unversioned directory is told from an empty
	// one, so they live here rather than being spelled out wherever each is opened.
	LogName  = "kvs.log"
	RaftName = "raft"
)

// ErrFormat is what every refusal to open a data directory matches. Use errors.As with
// FormatError to find out what was actually on disk.
var ErrFormat = errors.New("data directory format")

// FormatError says what the directory holds and what this build understands. Missing marks
// the case where there is no format file at all, which is not the same as one holding
// nothing, and Raw is whatever it did hold. The version is not kept as a number: zero is a
// perfectly good version to find written down, so it cannot double as "there was none".
type FormatError struct {
	Dir     string
	Missing bool
	Raw     string
}

func (e *FormatError) Error() string {
	if e.Missing {
		return fmt.Sprintf("%s holds kvs data but no %s file, so it was written before kvs "+
			"versioned its data directory; this build writes format %d. Move the directory "+
			"aside and start again.", e.Dir, FormatName, Version)
	}

	if _, err := strconv.Atoi(e.Raw); err != nil {
		return fmt.Sprintf("%s has a %s file holding %q, which is not a version number; this "+
			"build writes format %d. Move the directory aside and start again.",
			e.Dir, FormatName, e.Raw, Version)
	}

	return fmt.Sprintf("%s is format %s and this build understands format %d. kvs does not "+
		"convert between them: run the version that wrote it, or move the directory aside "+
		"and load the data again.", e.Dir, e.Raw, Version)
}

func (e *FormatError) Is(target error) bool {
	return target == ErrFormat
}

// Ensure makes dir usable by this build, creating it and stamping the current version when it
// is new. A directory belonging to another version comes back as a FormatError and nothing is
// touched: refusing to start is the whole point, because the alternative is a replay that
// half-works.
func Ensure(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dir, FormatName)

	raw, err := readFormat(path)

	switch {
	case errors.Is(err, os.ErrNotExist):
		// An empty directory is a new one. A directory already holding data without a version
		// was written before this check existed, and guessing which version it is would be
		// exactly the silent failure this package exists to prevent.
		found, dataErr := holdsData(dir)
		if dataErr != nil {
			return dataErr
		}

		if found {
			return &FormatError{Dir: dir, Missing: true}
		}

		return stamp(path)

	case err != nil:
		return fmt.Errorf("read data dir format: %w", err)
	}

	text := strings.TrimSpace(string(raw))

	if found, convErr := strconv.Atoi(text); convErr != nil || found != Version {
		return &FormatError{Dir: dir, Raw: text}
	}

	return nil
}

// maxFormatBytes is more room than a version number will ever need and less than is worth
// reading into memory. Something has replaced the file if it is longer, and the promise is a
// refusal that says so, not an allocation the size of whatever was put there.
const maxFormatBytes = 64

// readFormat returns what the format file holds, or as much of it as could be a version. The
// caller tells a missing file from an unreadable one, so the error is returned unwrapped.
func readFormat(path string) ([]byte, error) {
	//nolint:gosec // The path is the data directory the operator named; that is the feature.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	return io.ReadAll(io.LimitReader(file, maxFormatBytes))
}

// holdsData reports whether the directory already carries a keyspace. It looks for the two
// things kvs puts there by name rather than asking whether the directory is empty, because a
// freshly mounted volume is not empty — lost+found is there before kvs ever runs.
func holdsData(dir string) (bool, error) {
	for _, name := range []string{LogName, RaftName} {
		switch _, err := os.Stat(filepath.Join(dir, name)); {
		case err == nil:
			return true, nil

		case !errors.Is(err, os.ErrNotExist):
			// Anything other than "it is not there" leaves the question open, and answering it
			// with "no data" would stamp this build's version onto a directory that may hold an
			// older one's keyspace — the silent acceptance this package exists to prevent.
			return false, fmt.Errorf("look for %s in %s: %w", name, dir, err)
		}
	}

	return false, nil
}

// stamp writes the version through a temporary file and renames it into place, so an
// interrupted write leaves no format file rather than an unreadable one. The difference
// matters: no file on an empty directory is stamped again next time, whereas a truncated one
// is a version this build does not recognize and would refuse the directory forever.
func stamp(path string) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, FormatName+".*")
	if err != nil {
		return fmt.Errorf("write data dir format: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := writeAndSync(tmp, strconv.Itoa(Version)+"\n"); err != nil {
		return fmt.Errorf("write data dir format: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write data dir format: %w", err)
	}

	// The rename is a directory entry, and syncing the file did not make it durable. Without
	// this a crash can leave data that was flushed afterwards next to no marker at all, which
	// reads as a directory written before kvs versioned them and is refused from then on.
	return syncDir(dir)
}

func writeAndSync(file *os.File, text string) error {
	if _, err := file.WriteString(text); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

func syncDir(dir string) error {
	//nolint:gosec // The path is the data directory the operator named; that is the feature.
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open data dir: %w", err)
	}

	if err := handle.Sync(); err != nil {
		_ = handle.Close()

		return fmt.Errorf("sync data dir: %w", err)
	}

	return handle.Close()
}
