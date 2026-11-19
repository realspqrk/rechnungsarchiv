package app

// Shell is the single Wails-bound service. It forwards to Service and adds
// the native parts (dialogs, Explorer, browser). No business logic here.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/realspqrk/rechnungsarchiv"
	"github.com/realspqrk/rechnungsarchiv/internal/archive"
)

const (
	projectURL = rechnungsarchiv.ProjectURL
	// pricesURL is the README price table; prices live only there (AGENTS.md).
	pricesURL = projectURL + "#zeitstempel-dienste"
	licenseID = "Apache-2.0"
)

type Shell struct {
	svc   Service
	app   *application.App
	fake  string // scenario name when the fake service is used
	debug bool   // screenshot hooks (fake or test environment)
	mu    sync.Mutex
	// folders waits for the frontend (TakeFolders): the folder of
	// `rechnungsarchiv gui <ordner>`, of a later launch, and dropped ones.
	// Wails drops events nobody listens to, and the frontend listens only
	// after its boot, so the event "ordner-wartet" is just a hint.
	folders []FolderDrop
	hint    func() // emits "ordner-wartet"; nil: none (tests, before Run sets it)
}

// AboutInfo is shown in the "Über" dialog.
type AboutInfo struct {
	Version string `json:"version"`
	License string `json:"license"`
	Fake    string `json:"fake"`
	// Debug enables the frontend's screenshot hooks (sample data or the
	// testtsa environment only).
	Debug bool `json:"debug"`
}

// StatusChanged is emitted as event "status-geaendert" when background
// work (stamping on start, trusted-list refresh) changed the status.
type StatusChanged struct {
	Busy string `json:"busy"`
}

// FolderDrop is a folder handed to the window (command line, later launch,
// drop), taken with TakeFolders.
type FolderDrop struct {
	Folder string `json:"folder"`
	// Error is a German message when the drop cannot be used (files, several folders).
	Error string `json:"error"`
}

func (s *Shell) About() AboutInfo {
	return AboutInfo{Version: archive.Version, License: licenseID, Fake: s.fake, Debug: s.debug}
}

// TakeFolders returns the folders handed over since the last call (oldest
// first) and empties the queue. The frontend calls it once after its boot
// and on every "ordner-wartet" event; it decides when to offer them.
func (s *Shell) TakeFolders() []FolderDrop {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.folders
	s.folders = nil
	return d
}

// offer queues d for the frontend and hints it to take the queue.
func (s *Shell) offer(d FolderDrop) {
	s.mu.Lock()
	s.folders = append(s.folders, d)
	hint := s.hint
	s.mu.Unlock()
	if hint != nil {
		hint()
	}
}

func (s *Shell) setHint(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hint = f
}

// handoverKey names the folder in the single-instance data of a later
// launch: the absolute folder its own command line already checked
// (Options.Folder), never its raw argv.
const handoverKey = "folder"

// singleInstanceOptions makes this process the only window. A later launch
// sends its folder (if any) and exits inside application.New; this window
// comes to the front (front) and queues the folder after checking it again.
func singleInstanceOptions(folder string, s *Shell, front func()) *application.SingleInstanceOptions {
	o := &application.SingleInstanceOptions{
		UniqueID: "at.rechnungsarchiv.gui",
		OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
			front()
			if f, ok := handoverFolder(d.AdditionalData[handoverKey]); ok {
				s.offer(FolderDrop{Folder: f})
			}
		},
	}
	if folder != "" {
		o.AdditionalData = map[string]string{handoverKey: folder}
	}
	return o
}

// handoverFolder checks a folder received from another process: an
// absolute path of an existing folder. Anything else is ignored.
func handoverFolder(p string) (string, bool) {
	if p == "" || !filepath.IsAbs(p) {
		return "", false
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return "", false
	}
	return filepath.Clean(p), true
}

// ServiceShutdown runs when the application quits (window closed): running
// operations and background work are cancelled.
func (s *Shell) ServiceShutdown() error {
	if c, ok := s.svc.(interface{ Shutdown() }); ok {
		c.Shutdown()
	}
	return nil
}

// --- Service passthrough -------------------------------------------------

func (s *Shell) Status(ctx context.Context) (Status, error) { return s.svc.Status(ctx) }
func (s *Shell) CreateArchive(ctx context.Context, req CreateArchiveRequest) (Status, error) {
	return s.svc.CreateArchive(ctx, req)
}
func (s *Shell) OpenArchive(ctx context.Context, dir string) (Status, error) {
	return s.svc.OpenArchive(ctx, dir)
}
func (s *Shell) CloseArchive(ctx context.Context) error { return s.svc.CloseArchive(ctx) }
func (s *Shell) Preview(ctx context.Context, folder string) (Preview, error) {
	return s.svc.Preview(ctx, folder)
}
func (s *Shell) RetentionUntil(ctx context.Context, belegjahr, years int) (string, error) {
	return s.svc.RetentionUntil(ctx, belegjahr, years)
}
func (s *Shell) Archive(ctx context.Context, opts ArchiveOptions) (ArchiveResult, error) {
	return s.svc.Archive(ctx, opts)
}
func (s *Shell) StampPending(ctx context.Context, session int) (StampResult, error) {
	return s.svc.StampPending(ctx, session)
}
func (s *Shell) Verify(ctx context.Context) (CheckSummary, error) { return s.svc.Verify(ctx) }
func (s *Shell) VerifyDeposit(ctx context.Context, seq int) (DepositReport, error) {
	return s.svc.VerifyDeposit(ctx, seq)
}
func (s *Shell) ExportPlan(ctx context.Context, from, to int) (ExportPlan, error) {
	return s.svc.ExportPlan(ctx, from, to)
}
func (s *Shell) Export(ctx context.Context, opts ExportOptions) (ExportResult, error) {
	return s.svc.Export(ctx, opts)
}
func (s *Shell) Settings(ctx context.Context) (Settings, error) { return s.svc.Settings(ctx) }
func (s *Shell) SetSettings(ctx context.Context, u SettingsUpdate) (Settings, error) {
	return s.svc.SetSettings(ctx, u)
}
func (s *Shell) Providers(ctx context.Context) ([]Provider, error) { return s.svc.Providers(ctx) }
func (s *Shell) SetCredentials(ctx context.Context, c Credentials) (Settings, error) {
	return s.svc.SetCredentials(ctx, c)
}
func (s *Shell) Disclaimer(ctx context.Context) (DisclaimerText, error) {
	return s.svc.Disclaimer(ctx)
}
func (s *Shell) TakeOver(ctx context.Context, session int) error {
	return s.svc.TakeOver(ctx, session)
}

// deposit finds an Ablage of the open archive in the status (no check).
func (s *Shell) deposit(ctx context.Context, seq int) (Status, Deposit, error) {
	st, err := s.svc.Status(ctx)
	if err != nil {
		return st, Deposit{}, err
	}
	i := slices.IndexFunc(st.Deposits, func(d Deposit) bool { return d.Seq == seq })
	if i < 0 {
		return st, Deposit{}, fmt.Errorf("Ablage %04d ist nicht im Archiv.", seq)
	}
	return st, st.Deposits[i], nil
}

// --- Native dialogs ------------------------------------------------------

// PickFolder shows a folder dialog. purpose: "archiv" | "ablage" | "export".
// It returns "" when the user cancels.
func (s *Shell) PickFolder(purpose string) (string, error) {
	titles := map[string]string{
		"archiv": "Archivordner wählen",
		"ablage": "Ordner mit Rechnungen wählen",
		"export": "Ziel für den Export wählen",
	}
	title, ok := titles[purpose]
	if !ok {
		return "", fmt.Errorf("unknown purpose %q", purpose)
	}
	d := s.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(purpose != "ablage")
	return cancelIsEmpty(d.PromptForSingleSelection())
}

// PickP12 selects the PKCS#12 access certificate.
func (s *Shell) PickP12() (string, error) {
	d := s.app.Dialog.OpenFile().
		SetTitle("Zugangszertifikat wählen").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("Zertifikat (*.p12, *.pfx)", "*.p12;*.pfx")
	return cancelIsEmpty(d.PromptForSingleSelection())
}

// SaveProtocol asks where to save the Archivprotokoll of one Ablage and
// writes it. It returns the path, or "" when the user cancels.
func (s *Shell) SaveProtocol(ctx context.Context, seq int) (string, error) {
	_, d, err := s.deposit(ctx, seq)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%04d_%s.archivprotokoll.pdf", d.Seq, d.Name)
	path, err := cancelIsEmpty(s.app.Dialog.SaveFile().
		SetMessage("Archivprotokoll speichern").
		SetFilename(name).
		AddFilter("PDF (*.pdf)", "*.pdf").
		PromptForSingleSelection())
	if err != nil || path == "" {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		path += ".pdf"
	}
	if err := s.svc.SaveProtocol(ctx, seq, path); err != nil {
		return "", err
	}
	return path, nil
}

func cancelIsEmpty(path string, err error) (string, error) {
	if err != nil {
		// Wails reports a cancelled dialog as an error or an empty path.
		if strings.Contains(strings.ToLower(err.Error()), "cancel") {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

// --- Explorer and browser --------------------------------------------------

// RevealDeposit selects the Ablage ZIP in Windows Explorer.
func (s *Shell) RevealDeposit(ctx context.Context, seq int) error {
	st, d, err := s.deposit(ctx, seq)
	if err != nil {
		return err
	}
	path := filepath.Join(st.Archive.Dir, d.File)
	if _, err := os.Stat(path); err != nil || d.File == "" {
		return fmt.Errorf("Datei nicht gefunden: %s", path)
	}
	// explorer.exe exits with 1 even on success; only start errors count.
	return selectInExplorer(path).Start()
}

// OpenFolder opens an existing folder (e.g. the export target) in Explorer.
func (s *Shell) OpenFolder(path string) error {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("Ordner nicht gefunden: %s", path)
	}
	return exec.Command("explorer.exe", path).Start()
}

// OpenProviderWebsite opens the provider's website (account sign-up).
// Only URLs from the built-in provider list are opened.
func (s *Shell) OpenProviderWebsite(ctx context.Context, id string) error {
	ps, err := s.svc.Providers(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(ps, func(p Provider) bool { return p.ID == id })
	if i < 0 || !strings.HasPrefix(ps[i].Website, "https://") {
		return errors.New("Für diesen Anbieter ist keine Website hinterlegt.")
	}
	return s.app.Browser.OpenURL(ps[i].Website)
}

// OpenPrices opens the price table in the README (prices are never in the app).
func (s *Shell) OpenPrices() error { return s.app.Browser.OpenURL(pricesURL) }

// OpenIssues opens the project's issues ("Weitere Anbieter? Melden Sie sich").
func (s *Shell) OpenIssues() error { return s.app.Browser.OpenURL(rechnungsarchiv.IssuesURL) }

// OpenProject opens the project page.
func (s *Shell) OpenProject() error { return s.app.Browser.OpenURL(projectURL) }

// supportLinks are the only pages "Rechnungsarchiv unterstützen" opens.
var supportLinks = map[string]string{
	"github":      "https://github.com/sponsors/realspqrk",
	"kofi":        "https://ko-fi.com/spqrk",
	"paypal":      "https://paypal.me/realSPQRK",
	"phrasevault": "https://phrasevault.app",
}

// OpenSupport opens one of the fixed support pages by id.
func (s *Shell) OpenSupport(id string) error {
	u, ok := supportLinks[id]
	if !ok {
		return fmt.Errorf("Unbekannter Link: %s", id)
	}
	return s.app.Browser.OpenURL(u)
}

// handleDrop turns dropped paths into a FolderDrop.
func handleDrop(paths []string) FolderDrop {
	if len(paths) != 1 {
		return FolderDrop{Error: "Bitte ziehen Sie genau einen Ordner hierher."}
	}
	fi, err := os.Stat(paths[0])
	if err != nil {
		return FolderDrop{Error: "Der Ordner ist nicht erreichbar."}
	}
	if !fi.IsDir() {
		return FolderDrop{Error: "Bitte ziehen Sie einen Ordner hierher, keine einzelnen Dateien."}
	}
	return FolderDrop{Folder: paths[0]}
}
