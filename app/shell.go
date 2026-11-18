package main

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

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	projectURL = "https://github.com/realspqrk/rechnungsarchiv"
	// pricesURL is the README price table; prices live only there (AGENTS.md).
	pricesURL = projectURL + "#zeitstempel-dienste"
	licenseID = "Apache-2.0"
)

// version is set at build time: -ldflags "-X main.version=1.0.0".
var version = "0.1.0-dev"

type Shell struct {
	svc  Service
	app  *application.App
	fake string // scenario name when the fake service is used
}

// AboutInfo is shown in the "Über" dialog.
type AboutInfo struct {
	Version string `json:"version"`
	License string `json:"license"`
	Fake    string `json:"fake"`
}

// FolderDrop is emitted as event "ordner-abgelegt" when folders are dropped.
type FolderDrop struct {
	Folder string `json:"folder"`
	// Error is a German message when the drop cannot be used (files, several folders).
	Error string `json:"error"`
}

func (s *Shell) About() AboutInfo {
	return AboutInfo{Version: version, License: licenseID, Fake: s.fake}
}

// --- Service passthrough -------------------------------------------------

func (s *Shell) Status(ctx context.Context) (Status, error) { return s.svc.Status(ctx) }
func (s *Shell) CreateArchive(ctx context.Context, req CreateArchiveRequest) (Status, error) {
	return s.svc.CreateArchive(ctx, req)
}
func (s *Shell) OpenArchive(ctx context.Context, dir string, readOnly bool) (Status, error) {
	return s.svc.OpenArchive(ctx, dir, readOnly)
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
func (s *Shell) StampPending(ctx context.Context) (StampResult, error) {
	return s.svc.StampPending(ctx)
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
	r, err := s.svc.VerifyDeposit(ctx, seq)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%04d_%s.archivprotokoll.pdf", r.Deposit.Seq, r.Deposit.Name)
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
	r, err := s.svc.VerifyDeposit(ctx, seq)
	if err != nil {
		return err
	}
	if _, err := os.Stat(r.Path); err != nil {
		return fmt.Errorf("Datei nicht gefunden: %s", r.Path)
	}
	// explorer.exe exits with 1 even on success; only start errors count.
	return exec.Command("explorer.exe", "/select,"+r.Path).Start()
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

// OpenProject opens the project page.
func (s *Shell) OpenProject() error { return s.app.Browser.OpenURL(projectURL) }

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
