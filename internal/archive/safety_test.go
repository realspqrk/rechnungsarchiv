package archive

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// contender opens the archive like a second process would: its own
// Archive value, its own handles. Windows share modes and flock(2) locks
// apply per handle / open file description, so two contenders in one test
// process contend exactly like two processes.
func contender(t *testing.T, dir, user string) *Archive {
	t.Helper()
	c, err := open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	c.user = user
	return c
}

// Several writers find the same dead owner's lock and try to take it over
// at the same moment. Exactly one may win; nobody may delete the winner's
// lock.
func TestLockConcurrentTakeover(t *testing.T) {
	a, _ := setup(t)
	dead := fmt.Sprintf(`{"host":%q,"pid":2147483000,"since":"2026-11-18T08:00:00Z","user":"x"}`, a.host)
	if err := os.WriteFile(a.lockPath(), []byte(dead), 0o644); err != nil {
		t.Fatal(err)
	}
	const n = 4
	// Every contender that decides to take over waits here until all have
	// arrived (or a short timeout), which widens the race window maximally.
	var arrived sync.WaitGroup
	arrived.Add(n)
	allIn := make(chan struct{})
	go func() { arrived.Wait(); close(allIn) }()
	// Then they proceed one after another, so that a later one acts after
	// an earlier one has already installed its lock.
	var mu sync.Mutex
	order := map[*Archive]int{}
	hookTakeover = func(c *Archive) {
		mu.Lock()
		k, seen := order[c]
		if !seen {
			k = len(order)
			order[c] = k
			arrived.Done()
		}
		mu.Unlock()
		select {
		case <-allIn:
		case <-time.After(300 * time.Millisecond):
		}
		time.Sleep(time.Duration(k) * 50 * time.Millisecond)
	}
	t.Cleanup(func() { hookTakeover = nil })

	type result struct {
		user    string
		release func()
		err     error
	}
	results := make(chan result, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		c := contender(t, a.Dir, fmt.Sprintf("c%d", i))
		go func() {
			start.Wait()
			rel, err := c.lock()
			results <- result{c.user, rel, err}
		}()
	}
	start.Done()
	var winners []result
	for i := 0; i < n; i++ {
		r := <-results
		var le *LockedError
		switch {
		case r.err == nil:
			winners = append(winners, r)
		case errors.As(r.err, &le):
		default:
			t.Errorf("%s: unexpected error %v", r.user, r.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d writers hold the archive at once", len(winners))
	}
	w := winners[0]
	held, err := a.readLock()
	if err != nil || held.User != w.user {
		t.Fatalf("lock content %+v (%v), winner %s", held, err, w.user)
	}
	w.release()
	if _, err := os.Lstat(a.lockPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock left after release: %v", err)
	}
}

// helperEnv makes the test binary act as a second writer process.
const helperEnv = "RECHNUNGSARCHIV_LOCK_HELPER"

func TestLockHelperProcess(t *testing.T) {
	dir := os.Getenv(helperEnv)
	if dir == "" {
		t.Skip("helper process only")
	}
	a, err := open(dir, false)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	if _, err := a.lock(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("locked")
	bufio.NewReader(os.Stdin).ReadString('\n') // hold until killed
	os.Exit(0)
}

// A live writer in another process keeps the archive even if the user
// confirms a takeover; once that process dies, the lock is taken over
// silently.
func TestLockHeldByLiveProcess(t *testing.T) {
	a, _ := setup(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"="+a.Dir)
	stdin, _ := cmd.StdinPipe()
	defer stdin.Close()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	line, _ := bufio.NewReader(out).ReadString('\n')
	if line != "locked\n" && line != "locked\r\n" {
		t.Fatalf("helper: %q", line)
	}

	_, err := a.lock()
	var le *LockedError
	if !errors.As(err, &le) || le.PID != cmd.Process.Pid {
		t.Fatalf("live lock not reported: %v", err)
	}
	if err := a.TakeOver(le); err == nil {
		t.Fatal("TakeOver removed the lock of a live writer")
	}
	if held, err := a.readLock(); err != nil || held.PID != cmd.Process.Pid {
		t.Fatalf("live lock changed: %+v %v", held, err)
	}

	cmd.Process.Kill()
	cmd.Wait()
	release, err := a.lock()
	if err != nil {
		t.Fatalf("dead writer's lock not taken over: %v", err)
	}
	release()
}

// makeDirLink creates link → target as a junction on Windows (no special
// rights needed) or a symlink elsewhere.
func makeDirLink(t *testing.T, target, link string) {
	t.Helper()
	var err error
	if runtime.GOOS == "windows" {
		var out []byte
		out, err = exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%v: %s", err, out)
		}
	} else {
		err = os.Symlink(target, link)
	}
	if err != nil {
		t.Skipf("cannot create a directory link here: %v", err)
	}
}

// Recovery must never follow a link out of the archive folder.
func TestRecoveryDoesNotFollowLinks(t *testing.T) {
	a, _ := setup(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "fremd.tmp")
	if err := os.WriteFile(victim, []byte("nicht unseres"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeDirLink(t, outside, filepath.Join(a.Dir, "0007_Verknüpfung.nachweis"))
	makeDirLink(t, outside, filepath.Join(a.Dir, "0008_x.nachweis"))
	own := filepath.Join(a.Dir, "0009_echt.nachweis")
	os.Mkdir(own, 0o755)
	os.WriteFile(filepath.Join(own, "1.tsr.tmp"), []byte("crash"), 0o644)

	if _, err := Open(a.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("recovery deleted a file outside the archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(own, "1.tsr.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("own temp file not cleaned up")
	}
}

var errCrash = errors.New("simulated crash")

// A crash right after the commit rename must not leave a writable Ablage.
// Recovery also repairs a writable one.
func TestCommitCrashAfterRenameIsReadOnly(t *testing.T) {
	a, in := setup(t)
	a.rename = func(from, to string) (bool, error) {
		if ok, err := renameNoReplace(from, to); !ok {
			return ok, err
		}
		panic(errCrash)
	}
	func() {
		defer func() {
			if r := recover(); r != errCrash {
				panic(r)
			}
		}()
		a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{})
	}()
	final := filepath.Join(a.Dir, "0001_2026-10 Eingangsrechnungen.zip")
	fi, err := os.Stat(final)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o200 != 0 {
		t.Fatal("committed Ablage is writable after a crash")
	}

	os.Chmod(final, 0o644)
	if _, err := Open(a.Dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(final); fi.Mode().Perm()&0o200 != 0 {
		t.Fatal("recovery did not restore the read-only attribute")
	}
}

// A failure after publication (unlink of the temp name, directory sync)
// still returns the committed Deposit and completes the remaining steps.
func TestCommitPostPublicationError(t *testing.T) {
	a, in := setup(t)
	a.rename = func(from, to string) (bool, error) {
		if ok, err := renameNoReplace(from, to); !ok || err != nil {
			return ok, err
		}
		return true, errors.New("Verzeichnis nicht synchronisiert")
	}
	dep, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{})
	var ce *CommittedError
	if dep == nil || !errors.As(err, &ce) {
		t.Fatalf("deposit %v, err %v", dep, err)
	}
	if fi, err := os.Stat(dep.Path); err != nil || fi.Mode().Perm()&0o200 != 0 {
		t.Fatalf("Ablage missing or writable: %v", err)
	}
	if fi, err := os.Stat(dep.EvidenceDir); err != nil || !fi.IsDir() {
		t.Fatalf("no .nachweis: %v", err)
	}
	if ds, err := a.Deposits(); err != nil || len(ds) != 1 || ds[0].Err != nil {
		t.Fatalf("deposits %v %v", ds, err)
	}
}

// The commit rename also works for paths beyond MAX_PATH.
func TestRenameNoReplaceLongPath(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < 300 {
		dir = filepath.Join(dir, "sehr-langer-ordnername-fuer-das-archiv")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("long paths unavailable: %v", err)
	}
	src, dst := filepath.Join(dir, "x.tmp"), filepath.Join(dir, "x.zip")
	if err := os.WriteFile(src, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if ok, err := renameNoReplace(src, dst); !ok || err != nil {
		t.Fatalf("rename: %v %v", ok, err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
}
