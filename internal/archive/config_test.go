package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettersWriteCanonicalConfig(t *testing.T) {
	a, _ := setup(t)
	id := "mensatek"
	if err := a.SetProvider(&id); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCompany("  Neuer Name GmbH "); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRetentionDefault(10); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(a.Dir, configFile))
	want := fmt.Sprintf(`{"archive_id":"%s","company":"Neuer Name GmbH","format":1,"retention_default":10,"tsa":"mensatek"}`, a.Config.ArchiveID)
	if string(cfg) != want {
		t.Fatalf("config %s", cfg)
	}
	if b, err := Open(a.Dir); err != nil || *b.Config.TSA != "mensatek" || b.Config.RetentionDefault != 10 {
		t.Fatalf("reopen: %+v %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, configFile+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file left behind")
	}
	if err := a.SetProvider(nil); err != nil || a.Config.TSA != nil {
		t.Fatalf("unset: %v", err)
	}

	// Refused: unknown provider, bad values, read-only, foreign lock.
	bad := "kostenlos"
	if a.SetProvider(&bad) == nil || a.SetRetentionDefault(9) == nil || a.SetCompany("a\nb") == nil {
		t.Fatal("bad value accepted")
	}
	ro, _ := OpenReadOnly(a.Dir)
	if err := ro.SetRetentionDefault(7); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only: %v", err)
	}
	foreign := `{"host":"anderer-pc","pid":1,"since":"2026-11-18T08:00:00Z","user":"x"}`
	os.WriteFile(a.lockPath(), []byte(foreign), 0o644)
	var le *LockedError
	if err := a.SetRetentionDefault(7); !errors.As(err, &le) || le.Host != "anderer-pc" {
		t.Fatalf("foreign lock: %v", err)
	}
	if h, err := ro.LockHolder(); err != nil || h == nil || h.Host != "anderer-pc" {
		t.Fatalf("holder %+v %v", h, err)
	}
	os.Remove(a.lockPath())
	if h, err := ro.LockHolder(); err != nil || h != nil {
		t.Fatalf("no lock: %+v %v", h, err)
	}
}

func TestLockIsTheWriterLock(t *testing.T) {
	a, in := setup(t)
	release, err := a.Lock()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Open(a.Dir) // another writer (separate handles)
	var le *LockedError
	if _, err := b.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{}); !errors.As(err, &le) {
		t.Fatalf("archive while locked: %v", err)
	}
	if h, err := b.LockHolder(); err != nil || h == nil || h.PID != os.Getpid() {
		t.Fatalf("holder %+v %v", h, err)
	}
	release()
	if _, err := b.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{}); err != nil {
		t.Fatal(err)
	}
	if n, err := b.NextSequence(); err != nil || n != 2 {
		t.Fatalf("next %d %v", n, err)
	}
}

func TestResolveDefaults(t *testing.T) {
	a, in := setup(t)
	r, err := a.Resolve(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{})
	if err != nil || r.Name != "2026-10 Eingangsrechnungen" || r.Belegjahr != 2026 || r.RetentionYears != 7 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := a.Resolve(filepath.Join(in, "ohne Jahr"), Options{}); !errors.Is(err, ErrNeedBelegjahr) {
		t.Fatalf("no year: %v", err)
	}
	if _, err := a.Resolve(a.Dir, Options{Belegjahr: 2026}); err == nil {
		t.Fatal("archive folder itself accepted")
	}
	if _, err := a.Resolve(in, Options{Belegjahr: 2026, RetentionYears: 5}); !errors.Is(err, ErrRetention) {
		t.Fatalf("retention: %v", err)
	}
}

func TestRememberDefault(t *testing.T) {
	a, _ := setup(t)
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)         // Windows
	t.Setenv("XDG_CONFIG_HOME", cfg) // elsewhere
	if DefaultDir() != "" {
		t.Fatal("default without settings")
	}
	p, _ := settingsPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"gui":{"x":1}}`), 0o644)
	if err := RememberDefault(a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := DefaultDir(); got != a.Dir {
		t.Fatalf("default %q, want %q", got, a.Dir)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), `"gui"`) || !strings.Contains(string(data), `"archive"`) {
		t.Fatalf("other keys lost: %s", data)
	}
	os.Remove(filepath.Join(a.Dir, configFile))
	if DefaultDir() != "" {
		t.Fatal("default without rechnungsarchiv.json")
	}
}
