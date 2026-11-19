package app

// The real Service: internal/archive, stamp, verify, export, report, creds,
// trust and tsa, wired the way the CLI wires them (cmd/rechnungsarchiv).
// It decides nothing itself; map.go turns the core's results into DTOs.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/realspqrk/rechnungsarchiv"
	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/report"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Busy states (Status.Busy).
const (
	busyCheck = "pruefen"
)

type realService struct {
	sys  system
	root context.Context // cancelled on shutdown (window closed)
	stop context.CancelFunc
	bg   sync.WaitGroup
	// onChange is called when background work changed the status.
	onChange func()

	// write serializes everything that takes the archive's writer lock:
	// one writer per archive, also inside this process. Opening and
	// closing take it too; a writer captures the generation it was asked
	// for before it waits and gives up if that changed (beginWriteFor).
	write sync.Mutex

	mu        sync.Mutex
	autoOpen  bool // the remembered default archive was tried
	chosen    bool // the user opened, created or closed an archive: no auto-open
	gen       int  // increments with every open and close
	genStop   context.CancelFunc
	genCtx    context.Context
	ar        *archive.Archive
	trust     *trust.Store
	report    *verify.Report
	check     *CheckSummary
	reasons   map[string]verify.Reason // from the last stamping run
	rev       int                      // increments whenever reasons change
	recent    []archive.Recent         // "Zuletzt geöffnet", read on start, open and close
	hasRecent bool
	recentSeq int                  // the newest loadRecent wins
	statusRev int                  // Status.Rev
	due       stamp.Due            // Status.Due
	dueRev    int                  // increments with every provider or credential change
	roWhy     string               // why the open archive is view only
	locked    *archive.LockedError // last lock of another writer seen
	lockRev   int                  // increments whenever locked is set
	busy      string
	trustErr  error // last trusted-list refresh
}

func newRealService(sys system) *realService {
	root, stop := context.WithCancel(context.Background())
	return &realService{sys: sys, root: root, stop: stop}
}

// Shutdown cancels background work and running operations and waits
// briefly for them (evidence writes are append-only, so stopping is safe).
func (s *realService) Shutdown() {
	s.stop()
	done := make(chan struct{})
	go func() { s.bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// wait blocks until background work has finished (tests).
func (s *realService) wait() { s.bg.Wait() }

var (
	errReadOnly   = errors.New("Nur Ansicht: In diesen Ordner kann dieser PC nicht schreiben, es wird nichts verändert.")
	errNotOffered = errors.New("Dieser Anbieter wird derzeit nicht angeboten (noch nicht im Echtbetrieb erprobt). Weitere Anbieter? Melden Sie sich über die Projektseite.")
	errNoArchive  = errors.New("Kein Archiv geöffnet.")
	errReopened   = errors.New("Abgebrochen: Das Archiv wurde inzwischen neu geöffnet oder geschlossen. Es wurde nichts geändert.")
	// Typically the backup's parent folder was chosen instead of the archive.
	errNotArchive = errors.New("In diesem Ordner liegt kein Archiv. Wählen Sie den Ordner, in dem die Datei rechnungsarchiv.json und die Ablagen liegen (bei einer Sicherung meist ein Unterordner).")
	errSwitched   = errors.New("Abgebrochen: Das Archiv wurde inzwischen gewechselt – bitte erneut ausführen. Es wurde nichts geändert.")
)

// opCtx is ctx that also ends when the application shuts down.
func (s *realService) opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.root, cancel)
	return c, func() { stop(); cancel() }
}

type snapshot struct {
	ar     *archive.Archive
	trust  *trust.Store
	gen    int
	genCtx context.Context // cancelled when this archive is closed or reopened
	cfg    archive.Config  // copied under s.mu: setters replace ar.Config
}

func (s *realService) current() (snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ar == nil {
		return snapshot{}, errNoArchive
	}
	return snapshot{s.ar, s.trust, s.gen, s.genCtx, s.ar.Config}, nil
}

func (s *realService) writer() (snapshot, error) {
	c, err := s.current()
	if err == nil && c.ar.ReadOnly() {
		err = errReadOnly
	}
	return c, err
}

// beginWrite takes the writer slot for the archive that is current now, in
// normal mode (see beginWriteFor).
func (s *realService) beginWrite(ctx context.Context) (snapshot, context.Context, func(), error) {
	want, err := s.writer()
	if err != nil {
		return snapshot{}, nil, nil, err
	}
	return s.beginWriteFor(ctx, want)
}

// beginWriteFor takes the writer slot for want, the archive (generation)
// that was current when the user asked. want is captured before waiting:
// an open queued on the slot ahead of this writer switches to another
// archive while it waits, and a snapshot taken only after the wait would
// silently write into that one. After the wait the generation must still
// be want's (errSwitched otherwise) and not ended by an open or close that
// is still waiting (errReopened). The returned ctx ends with the request,
// the application and the generation; done releases everything.
func (s *realService) beginWriteFor(ctx context.Context, want snapshot) (snapshot, context.Context, func(), error) {
	s.write.Lock()
	c, err := s.current()
	switch {
	case err != nil || c.gen != want.gen || c.ar != want.ar:
		err = errSwitched
	case c.genCtx.Err() != nil:
		err = errReopened
	case c.ar.ReadOnly():
		err = errReadOnly
	}
	if err != nil {
		s.write.Unlock()
		return snapshot{}, nil, nil, err
	}
	octx, cancel := s.opCtx(ctx)
	stop := context.AfterFunc(c.genCtx, cancel)
	return c, octx, func() { stop(); cancel(); s.write.Unlock() }, nil
}

// uiErr turns core errors into what the GUI shows. A lock of another
// writer is remembered so that TakeOver can remove exactly that lock.
func (s *realService) uiErr(err error) error {
	var le *archive.LockedError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &le):
		s.mu.Lock()
		s.locked = le
		s.lockRev++
		s.mu.Unlock()
		return lockedError(le)
	case errors.Is(err, archive.ErrReadOnly):
		return errReadOnly
	case errors.Is(err, context.Canceled):
		return errors.New("Abgebrochen.")
	case errors.Is(err, archive.ErrNoArchive):
		return errNotArchive
	case errors.Is(err, fs.ErrPermission):
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return fmt.Errorf("Kein Schreibzugriff: %s", pe.Path)
		}
	case errors.Is(err, fs.ErrNotExist):
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return fmt.Errorf("Nicht gefunden: %s", pe.Path)
		}
	}
	return err
}

func lockedError(le *archive.LockedError) *UIError {
	return &UIError{Code: CodeLocked, Lock: lockInfo(le.LockInfo),
		Message: le.Error() + ". Läuft das Programm dort nicht mehr (z. B. nach einem Absturz), können Sie die Sperre übernehmen."}
}

func lockInfo(l archive.LockInfo) *LockInfo {
	return &LockInfo{Host: l.Host, User: l.User, Since: l.Since}
}

func (s *realService) notify() {
	if s.onChange != nil {
		s.onChange()
	}
}

// --- open / close ----------------------------------------------------------

func (s *realService) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	try := !s.autoOpen && !s.chosen && s.ar == nil
	s.autoOpen = true
	s.mu.Unlock()
	if try {
		// The GUI opens the remembered default archive on start, unless the
		// user has opened, created or closed one already.
		if dir := archive.DefaultDir(); dir != "" {
			if err := s.open(ctx, dir, true); err != nil {
				log.Printf("default archive %s: %v", dir, err)
			}
		}
	}
	// Start screen without an archive: the list is needed now. With an
	// archive open, open() loads it in the background.
	s.mu.Lock()
	load := !s.hasRecent && s.ar == nil
	s.mu.Unlock()
	if load {
		s.loadRecent()
	}
	return s.status(), nil
}

// status is the Status of what is open now; it never opens anything.
func (s *realService) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusRev++
	st := Status{SuggestedDir: s.suggestedDir(), Session: s.gen, Rev: s.statusRev}
	for _, r := range s.recent {
		if s.ar == nil || !strings.EqualFold(filepath.Clean(r.Dir), filepath.Clean(s.ar.Dir)) {
			st.Recent = append(st.Recent, RecentArchive{Dir: r.Dir, Company: r.Company})
		}
	}
	if s.ar == nil {
		return st
	}
	st.HasArchive = true
	st.Archive = ArchiveInfo{Dir: s.ar.Dir, Company: s.ar.Config.Company, ArchiveID: s.ar.Config.ArchiveID,
		ReadOnly: s.ar.ReadOnly(), ReadOnlyReason: s.roWhy, RetentionDefault: s.ar.Config.RetentionDefault}
	if id := s.ar.Config.TSA; id != nil {
		st.Provider = providerRef(*id)
	}
	if s.report != nil {
		for i := len(s.report.Deposits) - 1; i >= 0; i-- {
			st.Deposits = append(st.Deposits, depositOf(s.report, &s.report.Deposits[i]))
		}
		st.Findings = findingsOf(s.report)
	}
	st.Counts = countStates(st.Deposits)
	if s.check != nil {
		c := *s.check
		st.LastCheck = &c
	}
	st.TrustList = trustInfo(s.trust, s.trustErr, time.Now())
	st.Busy = s.busy
	st.Due = Due{Paid: s.due.Paid, Free: s.due.Free}
	if s.locked != nil {
		st.Lock = lockInfo(s.locked.LockInfo)
	}
	return st
}

// choose records that the user decided which archive is open (or none):
// the remembered default is no longer opened automatically.
func (s *realService) choose() {
	s.mu.Lock()
	s.chosen, s.autoOpen = true, true
	s.mu.Unlock()
}

// suggestedDir proposes a new, unused archive folder in the user's home
// (not Documents: it may be synced, spec §1).
func (s *realService) suggestedDir() string {
	home, err := s.sys.home()
	if err != nil {
		return ""
	}
	return unusedPath(filepath.Join(home, "Rechnungsarchiv"))
}

// unusedPath returns p, or "p 2", "p 3", … if p exists and is not empty.
func unusedPath(p string) string {
	for i := 1; i < 100; i++ {
		c := p
		if i > 1 {
			c = fmt.Sprintf("%s %d", p, i)
		}
		if des, err := os.ReadDir(c); errors.Is(err, fs.ErrNotExist) || err == nil && len(des) == 0 {
			return c
		}
	}
	return p
}

func isArchive(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "rechnungsarchiv.json"))
	return err == nil && fi.Mode().IsRegular()
}

func (s *realService) CreateArchive(ctx context.Context, req CreateArchiveRequest) (Status, error) {
	company := strings.TrimSpace(req.Company)
	if company == "" {
		return Status{}, errors.New("Bitte geben Sie den Namen der Firma ein.")
	}
	if strings.TrimSpace(req.Dir) == "" {
		return Status{}, errors.New("Bitte wählen Sie einen Archivordner.")
	}
	dir, err := filepath.Abs(req.Dir)
	if err != nil {
		return Status{}, err
	}
	if isArchive(dir) {
		return Status{}, errors.New("In diesem Ordner liegt bereits ein Archiv. Öffnen Sie es mit „Bestehendes Archiv öffnen“.")
	}
	if _, err := archive.Init(dir, company); err != nil {
		return Status{}, s.uiErr(err)
	}
	return s.OpenArchive(ctx, dir)
}

func (s *realService) OpenArchive(ctx context.Context, dir string) (Status, error) {
	s.choose()
	if err := s.open(ctx, dir, false); err != nil {
		return Status{}, err
	}
	return s.status(), nil
}

// open opens dir and makes it the current archive. auto: the remembered
// default on start, which gives way to any choice the user made meanwhile.
// A folder this PC cannot write to (write-protected share, stick, DVD)
// opens view only (spec §9), with the reason shown.
func (s *realService) open(ctx context.Context, dir string, auto bool) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("Bitte wählen Sie einen Archivordner.")
	}
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(filepath.Join(abs, "rechnungsarchiv.json")); errors.Is(err, fs.ErrNotExist) {
		return errNotArchive
	} else if err != nil {
		return err // e.g. no access: not a wrong folder
	} else if !fi.Mode().IsRegular() {
		return errNotArchive
	}
	openWrite := s.sys.openWrite
	if openWrite == nil {
		openWrite = archive.Open
	}
	var ar *archive.Archive
	var ts *trust.Store
	why := ""
	if ar, err = openWrite(abs); err == nil {
		ts, err = s.sys.trust(abs)
	} else if ro, rerr := archive.OpenReadOnly(abs); rerr == nil {
		// Opening for writing failed (it takes the lock and tidies up), the
		// configuration is fine: view only. No writing, no lock, no
		// timestamp; refreshed trusted lists go to a cache outside.
		log.Printf("open %s for writing: %v; view only", abs, err)
		ar, why = ro, s.uiErr(err).Error()
		ts, err = s.sys.trustRead(abs)
	}
	if err != nil {
		return s.uiErr(err)
	}
	readOnly := ar.ReadOnly()
	rep, err := verify.Archive(ctx, abs, verify.Options{Trust: ts})
	if err != nil {
		return s.uiErr(err)
	}
	// Nothing is stamped on open: the UI asks first (Status.Due).
	due := s.dueOf(ctx, ar, ar.Config, ts)
	// Another PC's lock is part of the first status: the GUI must not ask
	// "Jetzt holen" for an archive it can only wait for.
	var held *archive.LockedError
	if !readOnly {
		if held, err = ar.Holder(); err != nil {
			log.Printf("lock check: %v", err)
		}
	}

	// End the current generation first, so writers queued on the slot give
	// up, then switch while holding the slot.
	if !s.endGeneration(auto) {
		return nil
	}
	s.write.Lock()
	s.mu.Lock()
	if auto && s.chosen {
		s.mu.Unlock()
		s.write.Unlock()
		return nil
	}
	if s.genStop != nil {
		s.genStop()
	}
	s.gen++
	s.genCtx, s.genStop = context.WithCancel(s.root)
	s.ar, s.trust, s.report, s.check = ar, ts, rep, checkOf(rep)
	s.reasons, s.locked, s.busy, s.trustErr, s.due, s.roWhy = nil, held, busyCheck, nil, due, why
	gen, gctx := s.gen, s.genCtx
	s.mu.Unlock()
	s.write.Unlock()

	if !readOnly {
		if err := archive.RememberDefault(abs); err != nil {
			log.Printf("remember default archive: %v", err)
		}
		// In the background: a remembered archive on a disconnected
		// network drive must not hold up opening this one.
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			s.loadRecent()
			s.notify()
		}()
	}
	s.bg.Add(1)
	go s.background(gctx, gen)
	return nil
}

// endGeneration cancels the operations of the current archive. It returns
// false for an automatic open that a user's choice has overtaken.
func (s *realService) endGeneration(auto bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if auto && s.chosen {
		return false
	}
	if s.genStop != nil {
		s.genStop()
	}
	return true
}

// background runs after opening (spec §7): refresh the trusted lists,
// check again (which also counts Status.Due), tell the UI. It never
// stamps: the UI asks first.
func (s *realService) background(ctx context.Context, gen int) {
	defer s.bg.Done()
	defer func() {
		s.mu.Lock()
		if s.gen == gen {
			s.busy = ""
		}
		s.mu.Unlock()
		s.notify()
	}()
	s.mu.Lock()
	ar, ts := s.ar, s.trust
	if s.gen != gen {
		ar = nil
	}
	s.mu.Unlock()
	if ar == nil {
		return
	}
	s.observeLock(gen, true) // before the network: a change shows at once
	provider := ""
	if ar.Config.TSA != nil {
		provider = *ar.Config.TSA
	}
	err := ts.Refresh(ctx, verify.Territories(ts, provider)...)
	if ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if s.gen == gen {
		s.trustErr = err
	}
	s.mu.Unlock()
	if _, err := s.reverify(ctx, gen, false); err != nil && ctx.Err() == nil {
		log.Printf("background check: %v", err)
	}
}

// observeLock looks whether another PC holds or left the writer lock
// (spec §1), so the main screen shows it before the first write, and a
// lock released meanwhile no longer blocks. It only reads .lock: no writer
// slot, no lock taken (a CLI writing meanwhile is not disturbed). notify:
// tell the UI now; otherwise the caller's status carries it together with
// the counts checked after it.
func (s *realService) observeLock(gen int, notify bool) {
	c, err := s.writer()
	if err != nil || c.gen != gen {
		return
	}
	s.mu.Lock()
	rev := s.lockRev
	s.mu.Unlock()
	le, err := c.ar.Holder()
	if err != nil {
		log.Printf("lock check: %v", err)
		return
	}
	s.mu.Lock()
	// A takeover or write that saw the lock meanwhile knows better.
	changed := s.gen == gen && s.lockRev == rev && !sameLock(s.locked, le)
	if changed {
		s.locked = le
		s.lockRev++
	}
	s.mu.Unlock()
	if changed && notify {
		s.notify()
	}
}

func sameLock(a, b *archive.LockedError) bool {
	return a == nil && b == nil || a != nil && b != nil && a.LockInfo == b.LockInfo
}

// dueOf counts what "Jetzt holen" would do (stamp.Plan: the same choice of
// Ablagen and the same readiness as stamping). Zero when a run could only
// fail (no provider, credentials or trusted list): the question would be
// pointless. cfg is a copy taken under s.mu (setters replace ar.Config).
func (s *realService) dueOf(ctx context.Context, ar *archive.Archive, cfg archive.Config, ts *trust.Store) stamp.Due {
	d, err := stamp.Plan(ctx, ar, cfg, stamp.Options{Trust: ts, Creds: s.sys.creds, Lock: ar.Lock, RootCAs: s.sys.rootCAs})
	if err != nil {
		log.Printf("pending timestamps: %v", err)
	}
	return d
}

// loadRecent reads "Zuletzt geöffnet". Not on every Status: a remembered
// archive on a disconnected network drive would stall each poll.
func (s *realService) loadRecent() {
	s.mu.Lock()
	s.recentSeq++
	seq := s.recentSeq
	s.mu.Unlock()
	r := archive.RecentArchives() // file reads, outside s.mu
	s.mu.Lock()
	if seq == s.recentSeq { // an older, slower load must not win
		s.recent, s.hasRecent = r, true
	}
	s.mu.Unlock()
}

func (s *realService) CloseArchive(ctx context.Context) error {
	s.choose()
	s.endGeneration(false)
	s.write.Lock()
	defer s.write.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.genStop != nil {
		s.genStop()
	}
	s.gen++
	s.ar, s.trust, s.report, s.check, s.reasons, s.locked, s.busy, s.trustErr, s.due, s.roWhy = nil, nil, nil, nil, nil, nil, "", nil, stamp.Due{}, ""
	return nil
}

// --- verification ----------------------------------------------------------

// reverify checks the whole archive and keeps the report for Status. The
// whole set is needed even for one Ablage: inherited proof and key
// compromise are decided across all Ablagen (spec §6 steps 7–8).
func (s *realService) reverify(ctx context.Context, gen int, online bool) (*verify.Report, error) {
	s.mu.Lock()
	if s.gen != gen || s.ar == nil {
		s.mu.Unlock()
		return nil, errNoArchive
	}
	ar, dir, ts, rev, cfg, dueRev := s.ar, s.ar.Dir, s.trust, s.rev, s.ar.Config, s.dueRev
	reasons := make(map[string]verify.Reason, len(s.reasons))
	for k, v := range s.reasons {
		reasons[k] = v
	}
	s.mu.Unlock()
	rep, err := verify.Archive(ctx, dir, verify.Options{Trust: ts, Online: online, Reasons: reasons})
	if err != nil {
		return nil, s.uiErr(err)
	}
	due := s.dueOf(ctx, ar, cfg, ts)
	s.mu.Lock()
	// A check that started with reasons dropped since then must not bring
	// them back, nor a count from before a provider or credential change.
	if s.gen == gen && s.rev == rev {
		s.report, s.check = rep, checkOf(rep)
	}
	// Also not one from before a stamping run (s.rev) finished meanwhile.
	if s.gen == gen && s.rev == rev && s.dueRev == dueRev {
		s.due = due
	}
	s.mu.Unlock()
	return rep, nil
}

func (s *realService) Verify(ctx context.Context) (CheckSummary, error) {
	c, err := s.current()
	if err != nil {
		return CheckSummary{}, err
	}
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	// "Archiv kontrollieren" also looks at the lock again and asks online
	// (spec §6 step 7, §7).
	s.observeLock(c.gen, false) // the UI reads the status after the check
	rep, err := s.reverify(ctx, c.gen, true)
	if err != nil {
		return CheckSummary{}, err
	}
	return *checkOf(rep), nil
}

func (s *realService) VerifyDeposit(ctx context.Context, seq int) (DepositReport, error) {
	c, err := s.current()
	if err != nil {
		return DepositReport{}, err
	}
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	rep, err := s.reverify(ctx, c.gen, false)
	if err != nil {
		return DepositReport{}, err
	}
	d, err := findDeposit(rep, seq)
	if err != nil {
		return DepositReport{}, err
	}
	return depositReportOf(rep, d, c.ar.ReadOnly(), c.ar.Dir), nil
}

func findDeposit(rep *verify.Report, seq int) (*verify.Deposit, error) {
	var found *verify.Deposit
	for i := range rep.Deposits {
		if rep.Deposits[i].Sequence == seq {
			if found != nil {
				return nil, fmt.Errorf("Nummer doppelt vergeben: Ablage %s. Bitte beide Ablagen mit „Archiv kontrollieren“ prüfen.", present.Number(seq))
			}
			found = &rep.Deposits[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("Ablage %s ist nicht im Archiv.", present.Number(seq))
	}
	return found, nil
}

func (s *realService) SaveProtocol(ctx context.Context, seq int, path string) error {
	c, err := s.current()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	outside := func(p string) error { return outsideArchive(p, c.ar.Dir) }
	if err := outside(abs); err != nil {
		return err
	}
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	rep, err := s.reverify(ctx, c.gen, false)
	if err != nil {
		return err
	}
	d, err := findDeposit(rep, seq)
	if err != nil {
		return err
	}
	pdf, err := report.PDF(rep, d, report.Options{})
	if err != nil {
		return err
	}
	// The save dialog already asked before replacing an existing file.
	return replaceFile(abs, pdf, outside)
}

var errIntoArchive = errors.New("Das Archivprotokoll wird nie in den Archivordner gespeichert. Bitte wählen Sie einen anderen Ordner.")

// outsideArchive refuses a target inside the archive, compared as written
// and where the paths physically lead (junctions, links), like the CLI. The
// target is also judged on its own: no ancestor of it, as written or
// physically, may be any archive (not only the open one).
func outsideArchive(target, archiveDir string) error {
	if within(target, archiveDir) {
		return errIntoArchive
	}
	phys, err := export.Physical(target)
	if err != nil {
		return err
	}
	for _, p := range []string{target, phys} {
		for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
			if isArchive(dir) {
				return errIntoArchive
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	pt, err1 := export.Physical(target)
	pa, err2 := export.Physical(archiveDir)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if within(pt, pa) {
		return errIntoArchive
	}
	return nil
}

// replaceFile writes data to path through a temp file in the same folder.
// check runs again on the created temp file, so a junction swapped in
// meanwhile is caught before anything is written.
func replaceFile(path string, data []byte, check func(string) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = check(tmp)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// --- archiving and stamping --------------------------------------------------

func (s *realService) Preview(ctx context.Context, folder string) (Preview, error) {
	c, err := s.writer()
	if err != nil {
		return Preview{}, err
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return Preview{}, err
	}
	out := Preview{Folder: abs, Session: c.gen, RetentionDefault: c.cfg.RetentionDefault, RetentionOptions: retentionOptions(),
		SuggestedName: filepath.Base(abs), SuggestedYear: archive.BelegjahrFromName(filepath.Base(abs))}
	if out.NextSeq, err = c.ar.NextSequence(); err != nil {
		return Preview{}, s.uiErr(err)
	}
	// The GUI names the Ablage after the folder; a name the archive cannot
	// use blocks it like a file problem (rename the folder).
	if archive.NameProblem(out.SuggestedName) != "" {
		out.Problems = append(out.Problems, PreviewProblem{Path: out.SuggestedName, Kind: "ordnername"})
	}
	p, err := c.ar.Preview(abs)
	if err != nil {
		if prob, ok := previewProblem(err); ok {
			out.Problems = append(out.Problems, prob)
		} else if !errors.Is(err, bag.ErrEmpty) {
			return Preview{}, s.uiErr(err)
		}
		if out.SuggestedYear == 0 {
			out.SuggestedYear = time.Now().Year()
		}
		return out, nil
	}
	previewOf(&out, p)
	return out, nil
}

func (s *realService) RetentionUntil(ctx context.Context, belegjahr, years int) (string, error) {
	if belegjahr < 1900 || belegjahr > 2999 || !slices.ContainsFunc(retentionOptions(), func(o RetentionOption) bool { return o.Years == years }) {
		return "", errors.New("Ungültiges Belegjahr oder ungültige Frist.")
	}
	return bag.RetentionUntil(belegjahr, years), nil
}

// runStamp fetches the missing timestamps (of roots only, if not nil)
// under the archive's writer lock. Callers hold s.write.
func (s *realService) runStamp(ctx context.Context, ar *archive.Archive, ts *trust.Store, roots []string) (*stamp.Result, error) {
	return stamp.Run(ctx, ar, stamp.Options{Trust: ts, Creds: s.sys.creds, Lock: ar.Lock, RootCAs: s.sys.rootCAs, Roots: roots})
}

// noteStamp keeps the reasons of a stamping run (verify.Options.Reasons) and
// the lock it ran into, if any.
func (s *realService) noteStamp(gen int, res *stamp.Result, err error) {
	var le *archive.LockedError
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return
	}
	switch {
	case errors.As(err, &le):
		s.locked = le
		s.lockRev++
	case err == nil && res != nil:
		s.reasons = res.Reasons()
		s.rev++
		s.locked = nil
		s.lockRev++
	}
}

func (s *realService) Archive(ctx context.Context, opts ArchiveOptions) (ArchiveResult, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return ArchiveResult{}, errors.New("Bitte geben Sie der Ablage einen Namen.")
	}
	// The archive the user archives into: captured now, before waiting
	// for the writer slot (beginWriteFor).
	want, err := s.writer()
	if err != nil {
		return ArchiveResult{}, err
	}
	// beginWriteFor checks under the lock that want is still current.
	if opts.Session != want.gen {
		return ArchiveResult{}, errSwitched
	}
	folder, err := filepath.Abs(opts.Folder)
	if err != nil {
		return ArchiveResult{}, err
	}
	if fi, err := os.Stat(folder); err != nil || !fi.IsDir() {
		return ArchiveResult{}, fmt.Errorf("Der Ordner ist nicht erreichbar: %s", folder)
	}
	c, ctx, done, err := s.beginWriteFor(ctx, want)
	if err != nil {
		return ArchiveResult{}, err
	}
	defer done()
	r, err := c.ar.Resolve(folder, archive.Options{Name: strings.TrimSpace(opts.Name), Belegjahr: opts.Belegjahr, RetentionYears: opts.RetentionYears})
	if err != nil {
		return ArchiveResult{}, s.uiErr(err)
	}
	// The last chance to stop: after this the Ablage is committed.
	if err := ctx.Err(); err != nil {
		return ArchiveResult{}, s.uiErr(err)
	}
	// Like the CLI: commit, then stamp. A stamping failure leaves the
	// Ablage committed and pending.
	dep, err := c.ar.Archive(folder, r)
	var ce *archive.CommittedError
	var warn []string
	switch {
	case errors.As(err, &ce):
		warn = append(warn, ce.Error())
	case err != nil:
		return ArchiveResult{}, s.uiErr(err)
	}
	// From here on the result always says committed: an error would let
	// the user archive the same folder again (a second Ablage, a second
	// paid timestamp).
	again := fmt.Sprintf("Die Ablage %s ist gespeichert; bitte nicht erneut archivieren.", present.Number(dep.Sequence))
	// Only the new Ablage: older pending ones only on request (the
	// question on opening, "Jetzt nachholen").
	res, serr := s.runStamp(ctx, c.ar, c.trust, []string{dep.Bag.Root})
	s.noteStamp(c.gen, res, serr)
	if serr != nil {
		warn = append(warn, again+" Der Zeitstempel konnte nicht angefordert werden: "+s.uiErr(serr).Error())
	}
	warn = append(warn, stampResultOf(res).Warnings...)
	rep, err := s.reverify(ctx, c.gen, false)
	i := -1
	if err == nil {
		i = slices.IndexFunc(rep.Deposits, func(d verify.Deposit) bool { return d.Root == dep.Bag.Root })
	}
	s.notify()
	if i < 0 {
		// Not checked again (cancelled, archive closed, …): say only what
		// is certain.
		why := "sie ist nicht mehr im Archiv"
		if err != nil {
			why = err.Error()
		}
		if serr == nil {
			warn = append(warn, again)
		}
		warn = append(warn, "Ihr Zustand konnte nicht mehr geprüft werden ("+strings.TrimSuffix(why, ".")+"); „Archiv kontrollieren“ zeigt ihn.")
		return ArchiveResult{Committed: true, Deposit: committedDeposit(dep), Warning: strings.Join(warn, " ")}, nil
	}
	return ArchiveResult{Committed: true, Deposit: depositOf(rep, &rep.Deposits[i]), Warning: strings.Join(warn, " ")}, nil
}

// committedDeposit describes a committed Ablage that was not checked
// again: pending, since no timestamp is known to be valid.
func committedDeposit(dep *archive.Deposit) Deposit {
	out := Deposit{Seq: dep.Sequence, Name: dep.Name, State: StatePending, File: filepath.Base(dep.Path)}
	if b := dep.Bag; b != nil {
		out.Files = len(b.Manifest)
		for _, f := range b.Files {
			out.Bytes += f.Size
		}
		if b.Info != nil {
			out.Belegjahr = b.Info.Belegjahr
		}
	}
	return out
}

func (s *realService) StampPending(ctx context.Context, session int) (StampResult, error) {
	want, err := s.writer()
	if err != nil {
		return StampResult{}, err
	}
	if session != want.gen {
		return StampResult{}, errSwitched // asked for an archive no longer open
	}
	c, ctx, done, err := s.beginWriteFor(ctx, want)
	if err != nil {
		return StampResult{}, err
	}
	defer done()
	res, err := s.runStamp(ctx, c.ar, c.trust, nil)
	s.noteStamp(c.gen, res, err)
	if err != nil {
		return StampResult{}, s.uiErr(err)
	}
	if _, err := s.reverify(ctx, c.gen, false); err != nil {
		return StampResult{}, err
	}
	s.notify()
	return stampResultOf(res), nil
}

func (s *realService) TakeOver(ctx context.Context, session int) error {
	c, _, done, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer done()
	if session != c.gen {
		return errSwitched // the prompt was for an archive no longer open
	}
	s.mu.Lock()
	le := s.locked
	s.mu.Unlock()
	if le == nil {
		return errors.New("Es gibt keine Sperre, die übernommen werden könnte.")
	}
	if err := c.ar.TakeOver(le); err != nil {
		var still *archive.LockedError
		if errors.As(err, &still) {
			s.mu.Lock()
			s.locked = still
			s.lockRev++
			s.mu.Unlock()
			return &UIError{Code: CodeLocked, Lock: lockInfo(still.LockInfo),
				Message: still.Error() + ". Das Programm läuft dort noch; die Sperre wird erst frei, wenn es beendet ist."}
		}
		return s.uiErr(err)
	}
	s.mu.Lock()
	if s.locked == le {
		s.locked = nil
		s.lockRev++
	}
	s.mu.Unlock()
	// The opening question may follow now: count with today's clock (a
	// free completion's 24 h may have passed while the lock was shown).
	s.refreshDue(ctx, c.gen)
	s.notify()
	return nil
}

// --- export ------------------------------------------------------------------

func (s *realService) ExportPlan(ctx context.Context, from, to int) (ExportPlan, error) {
	c, err := s.current()
	if err != nil {
		return ExportPlan{}, err
	}
	s.mu.Lock()
	rep := s.report
	s.mu.Unlock()
	if rep == nil {
		if rep, err = s.reverify(ctx, c.gen, false); err != nil {
			return ExportPlan{}, err
		}
	}
	p := exportPlanOf(rep, from, to)
	p.Session = c.gen
	if home, err := s.sys.home(); err == nil {
		base := home
		if fi, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && fi.IsDir() {
			base = filepath.Join(home, "Downloads")
		}
		p.SuggestedTarget = unusedPath(filepath.Join(base, exportName(p.From, p.To)))
	}
	return p, nil
}

func exportName(from, to int) string {
	if from == to {
		return fmt.Sprintf("Betriebspruefung-%d", from)
	}
	return fmt.Sprintf("Betriebspruefung-%d-%d", from, to)
}

func (s *realService) Export(ctx context.Context, opts ExportOptions) (ExportResult, error) {
	if strings.TrimSpace(opts.Target) == "" {
		return ExportResult{}, errors.New("Bitte wählen Sie ein Ziel für den Export.")
	}
	if opts.From == 0 || opts.To == 0 || opts.From > opts.To {
		return ExportResult{}, errors.New("Bitte wählen Sie die Belegjahre.")
	}
	c, err := s.current()
	if err != nil {
		return ExportResult{}, err
	}
	if opts.Session != c.gen {
		return ExportResult{}, errSwitched
	}
	dst, err := filepath.Abs(opts.Target)
	if err != nil {
		return ExportResult{}, err
	}
	// A chosen folder that already holds files gets the export as a new
	// subfolder (the export itself is always a new, empty folder).
	if des, err := os.ReadDir(dst); err == nil && len(des) > 0 {
		dst = unusedPath(filepath.Join(dst, exportName(opts.From, opts.To)))
	}
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	res, err := export.Run(ctx, c.ar.Dir, dst, export.Options{From: opts.From, To: opts.To,
		SourceTrust: s.sys.trustRead, ExportTrust: s.sys.trustExport, Executable: s.sys.verifier})
	if err != nil {
		return ExportResult{}, s.uiErr(err)
	}
	out := ExportResult{Target: res.Dir, Deposits: len(res.Exported), Warnings: slices.Clone(res.Warnings)}
	if res.Report != nil && (res.Report.Status != verify.StatusTimestamped || !res.Report.ChainOK()) {
		out.Warnings = append(out.Warnings, "Die Kontrolle des Exports ergab: "+present.Status(res.Report.Status)+". Details zeigt rechnungsarchiv.exe verify im Exportordner.")
	}
	return out, nil
}

// --- settings ----------------------------------------------------------------

func (s *realService) Settings(ctx context.Context) (Settings, error) {
	c, err := s.current()
	if err != nil {
		return Settings{}, err
	}
	return s.settingsOf(c), nil
}

func (s *realService) settingsOf(c snapshot) Settings {
	cfg := c.ar.Config
	out := Settings{Company: cfg.Company, ArchiveDir: c.ar.Dir, RetentionDefault: cfg.RetentionDefault,
		RetentionOptions: retentionOptions(), TrustListDate: trustInfo(c.trust, nil, time.Now()).Date}
	if cfg.TSA != nil {
		out.ProviderID = *cfg.TSA
		// Never the secret itself: only whether one is stored.
		if cr, err := s.sys.creds.Get(cfg.ArchiveID, *cfg.TSA); err == nil {
			out.Credentials = CredentialState{User: cr.User, HasPassword: cr.Password != "", P12Path: cr.P12Path, URL: cr.URL}
		}
	}
	return out
}

func (s *realService) SetSettings(ctx context.Context, u SettingsUpdate) (Settings, error) {
	c, ctx, done, err := s.beginWrite(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer done()
	// The archive's Config changes; Status reads it under s.mu.
	config := func(set func() error) error {
		s.mu.Lock()
		err := set()
		s.mu.Unlock()
		return s.uiErr(err)
	}
	if u.Company != nil {
		name := strings.TrimSpace(*u.Company)
		if name == "" {
			return Settings{}, errors.New("Bitte geben Sie den Namen der Firma ein.")
		}
		if err := config(func() error { return c.ar.SetCompany(name) }); err != nil {
			return Settings{}, err
		}
	}
	if u.RetentionDefault != nil {
		if err := config(func() error { return c.ar.SetRetentionDefault(*u.RetentionDefault) }); err != nil {
			return Settings{}, err
		}
	}
	providerChanged := false
	if u.ProviderID != nil {
		// Only writers change the Config, and this one holds the slot.
		old := ""
		if c.ar.Config.TSA != nil {
			old = *c.ar.Config.TSA
		}
		providerChanged = old != *u.ProviderID
		var id *string
		if *u.ProviderID != "" {
			if p, ok := tsa.ProviderByID(*u.ProviderID); !ok {
				return Settings{}, errors.New("Unbekannter Anbieter.")
			} else if !p.Offered(old) {
				return Settings{}, errNotOffered
			}
			id = u.ProviderID
		}
		if err := config(func() error { return c.ar.SetProvider(id) }); err != nil {
			return Settings{}, err
		}
	}
	s.mu.Lock()
	s.locked = nil
	s.lockRev++
	s.mu.Unlock()
	if providerChanged {
		s.accessChanged(ctx, c.gen, true)
	}
	return s.settingsOf(c), nil
}

// accessChanged runs after the provider or its credentials changed: the
// reasons of the last stamping run ("kein-dienst", "zugang", …) belong to
// the old setup. The archive is checked again without them, so the status
// offers "Jetzt nachholen" at once instead of after a restart.
// A provider change (rare) always checks again: it decides "kein-dienst",
// also for a check still running. A saved credential field checks only
// when the last run left reasons to drop.
func (s *realService) accessChanged(ctx context.Context, gen int, provider bool) {
	s.mu.Lock()
	s.dueRev++ // counts begun before this change are dropped
	stale := s.gen == gen && (provider || s.report == nil || len(s.reasons) > 0)
	if stale {
		s.reasons = nil
		s.rev++
	}
	s.mu.Unlock()
	if !stale {
		// Credentials may be complete now: count Status.Due again.
		s.refreshDue(ctx, gen)
		return
	}
	if _, err := s.reverify(ctx, gen, false); err != nil && ctx.Err() == nil {
		log.Printf("check after provider change: %v", err)
	}
	s.notify()
}

// refreshDue counts Status.Due again without checking the archive.
func (s *realService) refreshDue(ctx context.Context, gen int) {
	s.mu.Lock()
	ar, ts := s.ar, s.trust
	if s.gen != gen || ar == nil {
		s.mu.Unlock()
		return
	}
	cfg, dueRev := ar.Config, s.dueRev
	s.mu.Unlock()
	due := s.dueOf(ctx, ar, cfg, ts)
	s.mu.Lock()
	if s.gen == gen && s.dueRev == dueRev {
		s.due = due
	}
	s.mu.Unlock()
	s.notify()
}

func (s *realService) Providers(ctx context.Context) ([]Provider, error) {
	cur := ""
	if c, err := s.current(); err == nil && c.cfg.TSA != nil {
		cur = *c.cfg.TSA
	}
	return offeredProviders(cur), nil
}

// SetCredentials stores the provider's credentials in the OS credential
// store (creds.System), like `provider set`. Empty secret fields keep the
// stored secret; a client certificate is checked with its password first.
func (s *realService) SetCredentials(ctx context.Context, in Credentials) (Settings, error) {
	c, ctx, done, err := s.beginWrite(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer done()
	cfg := c.ar.Config
	if cfg.TSA == nil {
		return Settings{}, errors.New("Bitte wählen Sie zuerst einen Anbieter.")
	}
	p, ok := tsa.ProviderByID(*cfg.TSA)
	if !ok {
		return Settings{}, fmt.Errorf("Zeitstempel-Anbieter %q ist dieser Version nicht bekannt.", *cfg.TSA)
	}
	old, err := s.sys.creds.Get(cfg.ArchiveID, p.ID)
	if err != nil && !errors.Is(err, creds.ErrNotFound) {
		return Settings{}, err
	}
	next := creds.Credential{URL: old.URL}
	switch p.Auth {
	case tsa.AuthClientCert:
		next.P12Path, next.Password = old.P12Path, old.Password
		if in.P12Path != "" {
			abs, err := filepath.Abs(in.P12Path)
			if err != nil {
				return Settings{}, err
			}
			next.P12Path = abs
		}
		if in.P12Password != "" {
			next.Password = in.P12Password
		}
		if next.P12Path != "" && next.Password != "" && (next.P12Path != old.P12Path || next.Password != old.Password) {
			data, err := os.ReadFile(next.P12Path)
			if err != nil {
				return Settings{}, fmt.Errorf("Zertifikatsdatei nicht lesbar: %s", next.P12Path)
			}
			if _, err := tsa.LoadClientCert(data, next.Password); err != nil {
				return Settings{}, errors.New("Zertifikatsdatei oder Zertifikatspasswort ungültig.")
			}
		}
	default:
		next.User, next.Password = strings.TrimSpace(in.User), old.Password
		if in.Password != "" {
			next.Password = in.Password
		}
	}
	if p.URLFromContract {
		url := strings.TrimSpace(in.URL)
		if url != "" && !strings.HasPrefix(url, "https://") {
			return Settings{}, errors.New("Die Adresse des Dienstes muss mit https:// beginnen.")
		}
		next.URL = url
	}
	if err := s.sys.creds.Set(cfg.ArchiveID, p.ID, next); err != nil {
		return Settings{}, err
	}
	if next != old {
		s.accessChanged(ctx, c.gen, false)
	}
	return s.settingsOf(c), nil
}

func (s *realService) Disclaimer(ctx context.Context) (DisclaimerText, error) {
	d := rechnungsarchiv.ParseDisclaimer(rechnungsarchiv.DisclaimerMarkdown)
	return DisclaimerText{Markdown: d.Markdown, Short: d.Short, Consent: d.Consent}, nil
}
