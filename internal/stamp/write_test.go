package stamp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOnceNoReplaceLongPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("a", 100), strings.Repeat("b", 100), strings.Repeat("c", 100))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "1.provider")
	if err := writeOnce(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writeOnce(p, []byte("x")); err != nil {
		t.Fatalf("identical rewrite: %v", err)
	}
	if err := writeOnce(p, []byte("y")); err == nil {
		t.Fatal("replaced an existing file")
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("%q", b)
	}
	des, _ := os.ReadDir(dir)
	if len(des) != 1 {
		t.Fatalf("temp files left: %v", des)
	}
}
