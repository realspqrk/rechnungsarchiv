package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	t.Setenv(ConfigDirEnv, cfg)
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

	// Recent: newest first, no duplicates (case-insensitive), at most
	// maxRecent, only folders that still hold an archive.
	var dirs []string
	for i := range maxRecent + 1 {
		d := filepath.Join(t.TempDir(), "A")
		if _, err := Init(d, "Firma "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
		if err := RememberDefault(d); err != nil {
			t.Fatal(err)
		}
	}
	again := dirs[3]
	if runtime.GOOS == "windows" {
		again = strings.ToUpper(again)
	}
	if err := RememberDefault(again); err != nil {
		t.Fatal(err)
	}
	got := RecentArchives()
	if len(got) != maxRecent || !samePath(got[0].Dir, dirs[3]) || got[0].Company != "Firma D" || got[1].Dir != dirs[maxRecent] {
		t.Fatalf("recent %+v", got)
	}
	os.Remove(filepath.Join(dirs[maxRecent], configFile))
	if got := RecentArchives(); len(got) != maxRecent-1 || slices.ContainsFunc(got, func(r Recent) bool { return r.Dir == dirs[maxRecent] }) {
		t.Fatalf("recent after removing an archive %+v", got)
	}
}

// RECHNUNGSARCHIV_CONFIG_DIR moves settings.json into that folder and
// leaves the user's real settings untouched; a relative value is refused
// without falling back to the real file.
func TestConfigDirOverride(t *testing.T) {
	a, _ := setup(t)
	// The OS location, isolated on every system (Windows APPDATA, Unix
	// XDG_CONFIG_HOME, macOS HOME), then derived the way settingsPath does.
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
	t.Setenv(ConfigDirEnv, "")
	base, err := os.UserConfigDir()
	if err != nil || !strings.HasPrefix(base, cfg) {
		t.Fatalf("UserConfigDir %q not isolated: %v", base, err)
	}
	real := filepath.Join(base, settingsDir, settingsFile)
	// The sentinel names a real archive, so reading it by mistake shows.
	other, err := Init(filepath.Join(t.TempDir(), "Echt"), "Echte Firma")
	if err != nil {
		t.Fatal(err)
	}
	if err := RememberDefault(other.Dir); err != nil {
		t.Fatal(err)
	}
	sentinel, err := os.ReadFile(real)
	if err != nil || DefaultDir() != other.Dir {
		t.Fatalf("sentinel %s %v", sentinel, err)
	}

	override := filepath.Join(t.TempDir(), "neu") // created on write
	t.Setenv(ConfigDirEnv, override)
	if DefaultDir() != "" {
		t.Fatal("default read from the real settings despite the override")
	}
	if err := RememberDefault(a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := DefaultDir(); got != a.Dir {
		t.Fatalf("default %q, want %q", got, a.Dir)
	}
	var m map[string]any
	if data, err := os.ReadFile(filepath.Join(override, settingsFile)); err != nil || json.Unmarshal(data, &m) != nil || m["archive"] != a.Dir {
		t.Fatalf("override settings %s %v", data, err)
	}
	if data, _ := os.ReadFile(real); string(data) != string(sentinel) {
		t.Fatalf("real settings changed: %s", data)
	}

	rel := "relativ"
	t.Chdir(t.TempDir())
	t.Setenv(ConfigDirEnv, rel)
	if err := RememberDefault(a.Dir); err == nil {
		t.Fatal("relative override accepted")
	}
	if DefaultDir() != "" {
		t.Fatal("relative override fell back to a default")
	}
	if _, err := os.Stat(rel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("relative override created %s: %v", rel, err)
	}
	if data, _ := os.ReadFile(real); string(data) != string(sentinel) {
		t.Fatalf("real settings changed: %s", data)
	}

	t.Setenv(ConfigDirEnv, "") // empty: the real file again
	if p, _ := settingsPath(); p != real || DefaultDir() != other.Dir {
		t.Fatalf("empty override: %s, want %s", p, real)
	}
}

// Settings written by 1.0.1 have only "archive": it becomes the first
// recent archive.
func TestRecentFromOldSettings(t *testing.T) {
	a, _ := setup(t)
	cfg := t.TempDir()
	t.Setenv(ConfigDirEnv, cfg)
	p, _ := settingsPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	old, _ := json.Marshal(map[string]string{"archive": a.Dir})
	os.WriteFile(p, old, 0o644)
	if got := RecentArchives(); len(got) != 1 || got[0].Dir != a.Dir {
		t.Fatalf("recent from old settings %+v", got)
	}
}
