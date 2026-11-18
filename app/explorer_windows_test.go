package app

import "testing"

// TestSelectInExplorerQuotesOnlyThePath: with the whole "/select,…" argument
// quoted, Explorer opened the Documents folder for Ablagen whose name has a
// space (every Ablage named after a folder like "2026-10 Eingangsrechnungen").
func TestSelectInExplorerQuotesOnlyThePath(t *testing.T) {
	cmd := selectInExplorer(`D:\Buchhaltung\Rechnungsarchiv\0001_2026-10 Eingangsrechnungen.zip`)
	want := `explorer.exe /select,"D:\Buchhaltung\Rechnungsarchiv\0001_2026-10 Eingangsrechnungen.zip"`
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine != want {
		t.Fatalf("command line %+v, want %s", cmd.SysProcAttr, want)
	}
}
