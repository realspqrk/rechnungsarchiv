package export

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// physical follows a junction to where it leads (Finding 3), so the
// containment check does not depend on noLinks alone.
func TestPhysicalFollowsJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows only")
	}
	base := t.TempDir()
	src := filepath.Join(base, "Archiv")
	os.Mkdir(src, 0o755)
	link := filepath.Join(base, "J")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, src).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	defer os.Remove(link)
	got, err := physical(filepath.Join(link, "neu", "export"))
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := physical(src)
	if !within(got, ps) || filepath.Base(got) != "export" {
		t.Fatalf("%s is not inside %s", got, ps)
	}
}
