package archive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The default archive is remembered per Windows user, outside any archive:
// <UserConfigDir>/Rechnungsarchiv/settings.json, key "archive". The CLI
// falls back to it when neither --archiv nor the current folder names an
// archive; the GUI opens it on start. It is a convenience, never evidence.

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

// RememberDefault stores dir (made absolute) as the default archive. Other
// keys in settings.json are kept.
func RememberDefault(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	m, p, err := readSettings()
	if err != nil {
		return err
	}
	v, err := json.Marshal(abs)
	if err != nil {
		return err
	}
	m["archive"] = v
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
