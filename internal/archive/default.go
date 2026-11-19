package archive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The default archive is remembered per Windows user, outside any archive:
// <UserConfigDir>/Rechnungsarchiv/settings.json, key "archive". The CLI
// falls back to it when neither --archiv nor the current folder names an
// archive; the GUI opens it on start and lists the archives under "recent"
// ("Zuletzt geöffnet"). It is a convenience, never evidence.

const (
	settingsDir  = "Rechnungsarchiv"
	settingsFile = "settings.json"
)

func settingsPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, settingsDir, settingsFile), nil
}

func readSettings() (map[string]json.RawMessage, string, error) {
	p, err := settingsPath()
	if err != nil {
		return nil, "", err
	}
	m := map[string]json.RawMessage{}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return m, p, nil
	}
	if err != nil {
		return nil, p, err
	}
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		m = map[string]json.RawMessage{} // a broken settings file is replaced
	}
	return m, p, nil
}

// DefaultDir returns the remembered default archive folder, or "" if none
// is remembered or it no longer contains rechnungsarchiv.json.
func DefaultDir() string {
	m, _, err := readSettings()
	if err != nil {
		return ""
	}
	var dir string
	if json.Unmarshal(m["archive"], &dir) != nil || dir == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, configFile)); err != nil {
		return ""
	}
	return dir
}

// maxRecent is how many archives "Zuletzt geöffnet" remembers.
const maxRecent = 6

// Recent is a remembered archive with the company named in it.
type Recent struct {
	Dir     string
	Company string
}

// RecentArchives returns the archives opened most recently, newest first
// (key "recent"). Folders that no longer hold a readable archive are left
// out; they come back once they are reachable again (e.g. a network drive).
func RecentArchives() []Recent {
	m, _, err := readSettings()
	if err != nil {
		return nil
	}
	var out []Recent
	for _, dir := range recentDirs(m) {
		if cfg, err := readConfig(dir); err == nil {
			out = append(out, Recent{Dir: dir, Company: cfg.Company})
		}
	}
	return out
}

// recentDirs reads "recent"; settings from 1.0.1 and older only have the
// default archive.
func recentDirs(m map[string]json.RawMessage) []string {
	var dirs []string
	if json.Unmarshal(m["recent"], &dirs) == nil && len(dirs) > 0 {
		return dirs
	}
	var dir string
	if json.Unmarshal(m["archive"], &dir) == nil && dir != "" {
		return []string{dir}
	}
	return nil
}

// samePath compares folder paths; on Windows case-insensitively.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// RememberDefault stores dir (made absolute) as the default archive and
// puts it first in the recent archives. Other keys in settings.json are
// kept.
func RememberDefault(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	m, p, err := readSettings()
	if err != nil {
		return err
	}
	recent := []string{abs}
	for _, d := range recentDirs(m) {
		if !samePath(d, abs) && len(recent) < maxRecent {
			recent = append(recent, d)
		}
	}
	for key, val := range map[string]any{"archive": abs, "recent": recent} {
		v, err := json.Marshal(val)
		if err != nil {
			return err
		}
		m[key] = v
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// A temp file of its own: the GUI and a CLI run may write at once (the
	// later rename wins; nobody reads a half-written file).
	f, err := os.CreateTemp(filepath.Dir(p), "settings-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, p)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
