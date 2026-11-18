package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// blockedStatus holds Status until release is closed (the frontend's boot
// awaits it before it listens for events).
type blockedStatus struct {
	Service
	entered chan struct{}
	release chan struct{}
}

func (b *blockedStatus) Status(ctx context.Context) (Status, error) {
	close(b.entered)
	<-b.release
	return Status{}, nil
}

func folders(ds []FolderDrop) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Folder)
	}
	return out
}

// Finding 1: a second `gui <ordner>` while the frontend still boots (its
// first Status is blocked, no listener yet, maybe no window) must not be
// lost. The folder waits in the Shell until the frontend takes it; the
// event is only a hint.
func TestSecondLaunchDuringBootIsQueued(t *testing.T) {
	start, later := t.TempDir(), t.TempDir()
	svc := &blockedStatus{entered: make(chan struct{}), release: make(chan struct{})}
	shell := &Shell{svc: svc}
	shell.offer(handleDrop([]string{start})) // Run: the folder of the first launch
	var hints atomic.Int32
	shell.setHint(func() { hints.Add(1) }) // emitted, but nobody listens yet
	var fronted atomic.Int32
	si := singleInstanceOptions("", shell, func() { fronted.Add(1) }) // no window yet

	booted := make(chan struct{})
	go func() {
		defer close(booted)
		_, _ = shell.Status(context.Background()) // boot: About, Disclaimer, Status
	}()
	<-svc.entered
	si.OnSecondInstanceLaunch(application.SecondInstanceData{
		Args:           []string{"rechnungsarchiv.exe", "gui", "--", later},
		AdditionalData: map[string]string{handoverKey: later},
	})
	if hints.Load() != 1 || fronted.Load() != 1 {
		t.Fatalf("hints %d, fronted %d", hints.Load(), fronted.Load())
	}
	close(svc.release)
	<-booted
	// After boot the frontend listens and takes the queue once.
	if got := folders(shell.TakeFolders()); !slices.Equal(got, []string{start, later}) {
		t.Fatalf("after boot: %q", got)
	}
	if got := shell.TakeFolders(); len(got) != 0 {
		t.Fatalf("taken twice: %+v", got)
	}
	// A drop while the window runs goes the same way.
	shell.offer(handleDrop([]string{later}))
	if got := folders(shell.TakeFolders()); !slices.Equal(got, []string{later}) || hints.Load() != 2 {
		t.Fatalf("drop: %q, hints %d", got, hints.Load())
	}
}

// Finding 2: the second launch hands over the folder its own CLI already
// parsed and checked (AdditionalData), not its raw argv. The first instance
// checks it again and ignores anything that is not an existing folder.
func TestSecondLaunchHandover(t *testing.T) {
	dir := t.TempDir()
	dash := filepath.Join(dir, "-ordner")
	os.Mkdir(dash, 0o755)
	file := filepath.Join(dir, "rechnung.pdf")
	os.WriteFile(file, []byte("%PDF"), 0o644)

	// The second instance sends its Options.Folder; round trip as JSON
	// like Wails does between the processes.
	sent := singleInstanceOptions(dash, &Shell{}, func() {})
	raw, _ := json.Marshal(application.SecondInstanceData{Args: []string{"x.exe", "gui", "--json=false", "--", "-ordner"}, WorkingDir: dir, AdditionalData: sent.AdditionalData})
	var got application.SecondInstanceData
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	first := &Shell{}
	recv := singleInstanceOptions("", first, func() {})
	recv.OnSecondInstanceLaunch(got)
	if f := folders(first.TakeFolders()); !slices.Equal(f, []string{dash}) {
		t.Fatalf("handover: %q", f)
	}
	if o := singleInstanceOptions("", first, func() {}); o.AdditionalData != nil {
		t.Fatalf("no folder, data %v", o.AdditionalData)
	}

	// Ignored: no folder (window only to the front), relative, missing,
	// a file, and a folder only in argv.
	for _, data := range []map[string]string{
		nil,
		{handoverKey: ""},
		{handoverKey: "ordner"},
		{handoverKey: filepath.Join(dir, "fehlt")},
		{handoverKey: file},
		{"other": dir},
	} {
		fronted := 0
		s := &Shell{}
		singleInstanceOptions("", s, func() { fronted++ }).OnSecondInstanceLaunch(application.SecondInstanceData{
			Args: []string{"x.exe", "gui", dir}, WorkingDir: dir, AdditionalData: data,
		})
		if q := s.TakeFolders(); len(q) != 0 || fronted != 1 {
			t.Errorf("%v: queued %+v, fronted %d", data, q, fronted)
		}
	}
}
