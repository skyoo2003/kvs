package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

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
	withVersion(t, "1.2.3", func() {
		out, errOut, err := runCLI(t, "-v")
		if err != nil {
			t.Fatalf("execute(-v) error = %v", err)
		}
		if errOut != "" {
			t.Fatalf("stderr = %q, want empty", errOut)
		}
		if out != "1.2.3\n" {
			t.Fatalf("stdout = %q, want %q", out, "1.2.3\n")
		}
	})
}

func TestExecuteVersionCommand(t *testing.T) {
	withVersion(t, "1.2.3", func() {
		out, errOut, err := runCLI(t, "version")
		if err != nil {
			t.Fatalf("execute(version) error = %v", err)
		}
		if errOut != "" {
			t.Fatalf("stderr = %q, want empty", errOut)
		}
		if out != "1.2.3\n" {
			t.Fatalf("stdout = %q, want %q", out, "1.2.3\n")
		}
	})
}

func TestExecuteMissingConfigFile(t *testing.T) {
	_, _, err := runCLI(t, "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("execute() error = nil, want config read error")
	}
}

func TestExecuteReadsConfigFile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
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

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := execute(args, &stdout, &stderr)

	return stdout.String(), stderr.String(), err
}

func withVersion(t *testing.T, want string, fn func()) {
	t.Helper()
	oldVersion := version
	version = want
	t.Cleanup(func() {
		version = oldVersion
	})
	fn()
}

func osWriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}
