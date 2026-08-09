package kvs

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"
)

// goldenPath holds the exported surface of this package. It is the machine-readable half of
// content/docs/compatibility.md: the page says what is promised, this file says what is there.
const goldenPath = "testdata/api-surface.txt"

// headerSep ends the human-facing preamble of the golden file. Everything after it is the
// surface itself, so the warning can be reworded without touching the comparison.
const headerSep = "# ---\n"

const surfaceHeader = `# Exported API surface promised for v1 - see content/docs/compatibility.md.
# A line changed or removed below is a breaking change and needs a major version.
# A line added is a new promise: it cannot be taken back within v1.
# Regenerate deliberately: go test -run TestPublicAPISurface . -update
` + headerSep

var updateSurface = flag.Bool("update", false, "rewrite "+goldenPath+" from the current source")

// TestPublicAPISurface fails when the exported surface of this package changes, so that
// widening or breaking the v1 promise is a deliberate act rather than a side effect of some
// other edit. Doc comments are deliberately not part of it: the parser is told to skip them,
// so rewording one costs nothing here.
func TestPublicAPISurface(t *testing.T) {
	got := exportedSurface(t)

	if *updateSurface {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("create testdata: %v", err)
		}

		if err := os.WriteFile(goldenPath, []byte(surfaceHeader+got), 0o600); err != nil {
			t.Fatalf("write %s: %v", goldenPath, err)
		}

		t.Logf("wrote %s", goldenPath)

		return
	}

	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with -update)", goldenPath, err)
	}

	_, want, ok := strings.Cut(string(raw), headerSep)
	if !ok {
		t.Fatalf("%s has no %q separator; regenerate with -update", goldenPath, headerSep)
	}

	if got != want {
		t.Errorf("exported API surface changed.\n%s\n\n"+
			"A changed or removed line is a breaking change; an added line is a new promise.\n"+
			"See content/docs/compatibility.md. If deliberate, regenerate with:\n"+
			"  go test -run TestPublicAPISurface . -update",
			firstDifference(want, got))
	}
}

// exportedSurface renders every exported declaration in the package directory, one block per
// file in name order so the output is stable across runs.
func exportedSurface(t *testing.T) string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()

	var buf bytes.Buffer

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		// Mode 0 leaves comments unparsed, which is what keeps a reworded doc comment from
		// failing this test: only the declarations are the promise.
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		// FileExports drops unexported declarations, struct fields, and interface methods.
		// It keeps the bodies of the exported functions, which is why signaturesOnly runs
		// after it: how a method is written is not what callers depend on.
		if !ast.FileExports(file) {
			continue
		}

		file.Decls = signaturesOnly(file.Decls)

		fmt.Fprintf(&buf, "==== %s ====\n", name)

		if err := printer.Fprint(&buf, fset, file); err != nil {
			t.Fatalf("print %s: %v", name, err)
		}

		buf.WriteString("\n\n")
	}

	// One trailing newline, which is what the repository's end-of-file hook normalises the
	// golden file to. Generating anything else makes committing it fail this test.
	return strings.TrimRight(buf.String(), "\n") + "\n"
}

// signaturesOnly drops the import block and every function body, leaving the declarations a
// caller can actually depend on. Without it an edit to the inside of an exported method would
// read as a change to the promise.
func signaturesOnly(decls []ast.Decl) []ast.Decl {
	kept := decls[:0]

	for _, decl := range decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
		case *ast.FuncDecl:
			d.Body = nil
		}

		kept = append(kept, decl)
	}

	return kept
}

// firstDifference points at the first line that does not match. The whole change is in
// `git diff` on the golden file; what a failing run needs is where to start looking.
func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		wantLine, gotLine := lineAt(wantLines, i), lineAt(gotLines, i)
		if wantLine != gotLine {
			return fmt.Sprintf("first difference at line %d:\n  promised: %q\n  found:    %q",
				i+1, wantLine, gotLine)
		}
	}

	return "no line differs, but the text does (trailing whitespace?)"
}

func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return ""
	}

	return lines[i]
}
