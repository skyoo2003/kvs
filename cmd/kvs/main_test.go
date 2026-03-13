package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

const testVersionOutput = "1.2.3\n"

func TestExecuteShowsHelpByDefault(t *testing.T) {
	out, errOut, err := runCLI(t)
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("stdout = %q, want usage text", out)
	}
	if !strings.Contains(out, "version") {
		t.Fatalf("stdout = %q, want version command in help", out)
	}
}

func TestExecuteVersionFlag(t *testing.T) {
	withVersion("1.2.3", func() {
		out, errOut, err := runCLI(t, "-v")
		if err != nil {
			t.Fatalf("execute(-v) error = %v", err)
		}
		if errOut != "" {
			t.Fatalf("stderr = %q, want empty", errOut)
		}
		if out != testVersionOutput {
			t.Fatalf("stdout = %q, want %q", out, testVersionOutput)
		}
	})
}

func TestExecuteVersionCommand(t *testing.T) {
	withVersion("1.2.3", func() {
		out, errOut, err := runCLI(t, "version")
		if err != nil {
			t.Fatalf("execute(version) error = %v", err)
		}
		if errOut != "" {
			t.Fatalf("stderr = %q, want empty", errOut)
		}
		if out != testVersionOutput {
			t.Fatalf("stdout = %q, want %q", out, testVersionOutput)
		}
	})
}

func TestExecuteMissingConfigFile(t *testing.T) {
	tempDir := mustTempDir(t)
	defer mustRemoveAll(t, tempDir)

	_, _, err := runCLI(t, "--config", filepath.Join(tempDir, "missing.yaml"))
	if err == nil {
		t.Fatal("execute() error = nil, want config read error")
	}
}

func TestExecuteReadsConfigFile(t *testing.T) {
	tempDir := mustTempDir(t)
	defer mustRemoveAll(t, tempDir)

	configFile := filepath.Join(tempDir, "config.yaml")
	if writeErr := osWriteFile(configFile, []byte("name: kvs\n")); writeErr != nil {
		t.Fatalf("osWriteFile() error = %v", writeErr)
	}

	out, errOut, err := runCLI(t, "--config", configFile, "version")
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if out != version+"\n" {
		t.Fatalf("stdout = %q, want %q", out, version+"\n")
	}
}

func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	defer viper.Reset()
	viper.Reset()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	err = execute(args, &stdoutBuf, &stderrBuf)

	return stdoutBuf.String(), stderrBuf.String(), err
}

func withVersion(want string, fn func()) {
	oldVersion := version
	version = want
	defer func() {
		version = oldVersion
	}()
	fn()
}

func osWriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}

func mustTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "kvs-cli-test-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}

	return dir
}

func mustRemoveAll(t *testing.T, path string) {
	t.Helper()

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("os.RemoveAll() error = %v", err)
	}
}
