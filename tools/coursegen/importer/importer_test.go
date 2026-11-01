package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, keep string) string {
	t.Helper()
	d := t.TempDir()
	c := filepath.Join(d, "01-test")
	os.MkdirAll(c, 0o755)
	os.WriteFile(filepath.Join(c, "manifest.yml"), []byte("id: 1\nname: \"Test O'Meadow\"\nstatus:\n  keep: "+keep+"\n"), 0o644)
	os.WriteFile(filepath.Join(c, "course_data.cdt"), []byte("cdt"), 0o644)
	os.WriteFile(filepath.Join(c, "thumbnail1.tnl"), []byte("tnl"), 0o644)
	return d
}

func opts(review, out string) Options {
	return Options{Review: review, Out: out, FirstID: 960000, Owner: 5, DataType: 3, Container: "raw"}
}

func TestOffByDefault(t *testing.T) {
	t.Setenv(EnvGate, "")
	if _, err := Stage(opts(fixture(t, "true"), t.TempDir())); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("want the off error, got %v", err)
	}
}

func TestNothingKeptNothingStaged(t *testing.T) {
	t.Setenv(EnvGate, EnvValue)
	if _, err := Stage(opts(fixture(t, "null"), t.TempDir())); err == nil {
		t.Fatal("a course that was not marked keep must not be staged")
	}
}

func TestRequiredOptions(t *testing.T) {
	t.Setenv(EnvGate, EnvValue)
	o := opts(fixture(t, "true"), t.TempDir())
	o.DataType = 0
	if _, err := Stage(o); err == nil {
		t.Fatal("data type must be required")
	}
}

func TestStage(t *testing.T) {
	t.Setenv(EnvGate, EnvValue)
	out := t.TempDir()
	n, err := Stage(opts(fixture(t, "true"), out))
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	sql, _ := os.ReadFile(filepath.Join(out, "import.sql"))
	if !strings.Contains(string(sql), "960000") || !strings.Contains(string(sql), "O''Meadow") || !strings.Contains(string(sql), "UNVERIFIED") {
		t.Fatalf("sql:\n%s", sql)
	}
	if _, err := os.Stat(filepath.Join(out, "objects", "960000.bin")); err != nil {
		t.Fatal(err)
	}
}
