package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Every scenario must load and serve the calls the screens make.
func TestFakeScenarios(t *testing.T) {
	ctx := context.Background()
	for _, name := range fakeScenarioNames() {
		f, err := newFakeService(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f.delay = 0
		s, err := f.Status(ctx)
		if err != nil {
			t.Fatalf("%s: status: %v", name, err)
		}
		if !s.HasArchive {
			continue
		}
		for _, d := range s.Deposits {
			if _, err := f.VerifyDeposit(ctx, d.Seq); err != nil {
				t.Fatalf("%s: verify %d: %v", name, d.Seq, err)
			}
		}
		if _, err := f.ExportPlan(ctx, 0, 0); err != nil {
			t.Fatalf("%s: export plan: %v", name, err)
		}
		if s.Archive.ReadOnly {
			if _, err := f.Archive(ctx, ArchiveOptions{Name: "x", Belegjahr: 2026, RetentionYears: 7}); err == nil {
				t.Fatalf("%s: read-only archive must refuse writing", name)
			}
		}
	}
	if _, err := newFakeService("gibt-es-nicht"); err == nil {
		t.Fatal("unknown scenario accepted")
	}
}

func TestHandleDrop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d := handleDrop([]string{dir}); d.Folder != dir || d.Error != "" {
		t.Fatalf("folder drop: %+v", d)
	}
	if d := handleDrop([]string{file}); d.Error == "" {
		t.Fatal("file drop accepted")
	}
	if d := handleDrop([]string{dir, dir}); d.Error == "" {
		t.Fatal("two folders accepted")
	}
}
