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

	// Opening it again has to be the ordinary case, not a second stamping.
	if err := datadir.Ensure(dir); err != nil {
		t.Errorf("Ensure() on its own directory error = %v", err)
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
