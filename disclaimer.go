// Package rechnungsarchiv holds files that live at the repository root and
// must be embedded into binaries. go:embed cannot reach parent directories,
// so the disclaimer is embedded here and imported by the GUI (and the CLI).
package rechnungsarchiv

import (
	_ "embed"
	"strings"
)

// DisclaimerMarkdown is docs/haftungsausschluss.md, the only source of the
// disclaimer (AGENTS.md). Never copy its text anywhere else.
//
//go:embed docs/haftungsausschluss.md
var DisclaimerMarkdown string

// Disclaimer is the disclaimer split into the parts the programs show.
type Disclaimer struct {
	// Markdown is the full document.
	Markdown string `json:"markdown"`
	// Short is the paragraph shown unchanged on first start and under "Über"
	// (the second paragraph of the section "Kurzfassung").
	Short string `json:"short"`
	// Consent is the sentence shown below Short ("Mit dem Anlegen …").
	Consent string `json:"consent"`
}

// ParseDisclaimer splits the embedded markdown. The first paragraph of
// "## Kurzfassung" describes how the program shows the text and ends with
// the consent sentence in „…“; the second paragraph is the short disclaimer.
func ParseDisclaimer(md string) Disclaimer {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	d := Disclaimer{Markdown: md}
	var paras []string
	in := false
	for _, block := range strings.Split(md, "\n\n") {
		block = strings.TrimSpace(block)
		if strings.HasPrefix(block, "## ") {
			if in {
				break
			}
			in = strings.TrimSpace(strings.TrimPrefix(block, "## ")) == "Kurzfassung"
			continue
		}
		if in && block != "" {
			paras = append(paras, strings.Join(strings.Fields(block), " "))
		}
	}
	if len(paras) >= 2 {
		d.Short = paras[1]
	}
	if len(paras) >= 1 {
		meta := paras[0]
		// The consent sentence is the last „…“ quote ("Darunter steht: „…“").
		if i := strings.LastIndex(meta, "„"); i >= 0 {
			rest := meta[i+len("„"):]
			if j := strings.Index(rest, "“"); j >= 0 {
				d.Consent = rest[:j]
			}
		}
	}
	return d
}
