package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndValidate(t *testing.T) {
	dir := t.TempDir()
	if err := generate([]string{"-out", dir, "-seed", "3"}); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(dir, "*", "course_data.cdt"))
	if len(m) != 60 {
		t.Fatalf("%d courses on disk", len(m))
	}
	if err := check([]string{dir}); err != nil {
		t.Fatal(err)
	}
	// Corrupt one file: validation must fail.
	b, _ := os.ReadFile(m[0])
	b[0x200] ^= 1
	os.WriteFile(m[0], b, 0o644)
	if err := check([]string{m[0]}); err == nil {
		t.Fatal("corrupt file passed")
	}
}
