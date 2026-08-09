package datadir_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skyoo2003/kvs/internal/datadir"
)

func TestEnsureStampsANewDirectory(t *testing.T) {
	dir := t.TempDir()

	if err := datadir.Ensure(dir); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	//nolint:gosec // The path is the temporary directory this test just made.
	raw, err := os.ReadFile(filepath.Join(dir, datadir.FormatName))
	if err != nil {
		t.Fatalf("read format: %v", err)
	}

	if got, want := strings.TrimSpace(string(raw)), "1"; got != want {
		t.Errorf("format = %q, want %q", got, want)
	}

	// The stamp is written under a temporary name and renamed into place. None of those names
	// may survive it, or the next reader finds a directory holding two answers.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	for _, entry := range entries {
		if entry.Name() != datadir.FormatName {
			t.Errorf("%s left behind after stamping", entry.Name())
		}
	}

	// Opening it again has to be the ordinary case, not a second stamping.
	if err := datadir.Ensure(dir); err != nil {
		t.Errorf("Ensure() on its own directory error = %v", err)
	}
}

// A stat that fails for any reason other than absence leaves the question open, and answering
// it with "nothing here" would stamp this build's version onto another one's keyspace.
func TestEnsureRefusesWhenItCannotTellWhetherDataIsThere(t *testing.T) {
	dir := t.TempDir()

	// A symlink pointing at itself is a path that exists and cannot be stat'd. It stands in for
	// the transient filesystem error this guards against, which a test cannot arrange directly.
	link := filepath.Join(dir, datadir.LogName)
	if err := os.Symlink(link, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	err := datadir.Ensure(dir)
	if err == nil {
		t.Fatal("Ensure() error = nil, want a refusal")
	}

	if errors.Is(err, datadir.ErrFormat) {
		t.Errorf("Ensure() error = %v, want the failure to look, not a verdict on the format", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, datadir.FormatName)); statErr == nil {
		t.Error("the directory was stamped without knowing what it holds")
	}
}

// A format file far larger than a version is refused for what it says, not loaded for its size.
func TestEnsureBoundsWhatItReadsFromTheFormatFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, datadir.FormatName), strings.Repeat("x", 1<<20))

	err := datadir.Ensure(dir)

	var formatErr *datadir.FormatError
	if !errors.As(err, &formatErr) {
		t.Fatalf("Ensure() error = %v, want a FormatError", err)
	}

	if len(formatErr.Raw) > 64 {
		t.Errorf("Raw is %d bytes long, so the whole file was read into memory", len(formatErr.Raw))
	}
}

func TestEnsureCreatesADirectoryThatIsNotThereYet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "kvs")

	if err := datadir.Ensure(dir); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, datadir.FormatName)); err != nil {
		t.Errorf("format file after Ensure: %v", err)
	}
}

// A directory holding data kvs cannot place is the case the whole package exists for. Each of
// these has to come back as ErrFormat, and the message has to say enough to act on.
func TestEnsureRefusesADirectoryItCannotRead(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, dir string)
		// wants are substrings the message must carry, so that a reader is told what is on
		// disk and what this build wanted.
		wants []string
	}{
		{
			name: "a later format",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				writeFormat(t, dir, "2")
			},
			wants: []string{"format 2", "format 1"},
		},
		{
			name: "an earlier format",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				writeFormat(t, dir, "0")
			},
			wants: []string{"format 0", "format 1"},
		},
		{
			name: "something that is not a version",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				writeFormat(t, dir, "banana")
			},
			wants: []string{`"banana"`, "not a version number"},
		},
		{
			// A file that exists and says nothing is not the same as no file, and the advice
			// for the two differs.
			name: "a format file holding nothing",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, datadir.FormatName), "")
			},
			wants: []string{`holding ""`, "not a version number"},
		},
		{
			name: "an append log written before versioning",
			arrange: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, datadir.LogName), "")
			},
			wants: []string{"no format file", "format 1"},
		},
		{
			name: "a raft directory written before versioning",
			arrange: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.MkdirAll(filepath.Join(dir, datadir.RaftName), 0o750); err != nil {
					t.Fatalf("create raft dir: %v", err)
				}
			},
			wants: []string{"no format file", "format 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.arrange(t, dir)

			err := datadir.Ensure(dir)
			if err == nil {
				t.Fatal("Ensure() error = nil, want a refusal")
			}

			if !errors.Is(err, datadir.ErrFormat) {
				t.Errorf("errors.Is(err, ErrFormat) = false, err = %v", err)
			}

			var formatErr *datadir.FormatError
			if !errors.As(err, &formatErr) {
				t.Fatalf("errors.As(err, *FormatError) = false, err = %v", err)
			}

			if formatErr.Dir != dir {
				t.Errorf("Dir = %q, want %q", formatErr.Dir, dir)
			}

			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message %q does not carry %q", err.Error(), want)
				}
			}
		})
	}
}

// A volume kvs has never touched is rarely empty, and refusing to start on one would be a
// worse failure than the one this package prevents.
func TestEnsureIgnoresFilesThatAreNotOurs(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "lost+found"), 0o750); err != nil {
		t.Fatalf("create lost+found: %v", err)
	}

	writeFile(t, filepath.Join(dir, "README"), "not ours")

	if err := datadir.Ensure(dir); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
}

func writeFormat(t *testing.T, dir, text string) {
	t.Helper()

	writeFile(t, filepath.Join(dir, datadir.FormatName), text+"\n")
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
