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

// ParseDisclaimer splits the embedded markdown. The section "## Kurzfassung"
// starts with an HTML comment for maintainers that ends with the consent
// sentence in „…“; the paragraph after it is the short disclaimer. Markdown
// is the document without HTML comments, as readers see it.
func ParseDisclaimer(md string) Disclaimer {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	d := Disclaimer{Markdown: stripComments(md)}
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

// stripComments removes <!-- … --> blocks and the blank lines after them.
func stripComments(md string) string {
	for {
		i := strings.Index(md, "<!--")
		if i < 0 {
			return md
		}
		j := strings.Index(md[i:], "-->")
		if j < 0 {
			return md
		}
		md = md[:i] + strings.TrimLeft(md[i+j+len("-->"):], "\n")
	}
}
