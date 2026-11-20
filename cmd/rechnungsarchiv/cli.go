package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
)

// Exit codes (ADR-0008). They are fixed; docs/cli.md lists them.
const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitConfig   = 3
	exitLocked   = 4
	exitFound    = 5 // Verändert / Ungültig (or a chain finding)
	exitPending  = 6 // Ausstehend / Unbestimmt
	exitProvider = 7
	exitConsent  = 8
)

// cliError is what a failed command reports: on stderr as
// {"error":{"code","message","hint","details"}} in JSON mode, as German
// text otherwise.
type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Details any    `json:"details,omitempty"`
	exit    int
}

func (e *cliError) Error() string { return e.Message }

func usageErr(msg, hint string) *cliError {
	if hint == "" {
		hint = "rechnungsarchiv --help"
	}
	return &cliError{Code: "USAGE", Message: msg, Hint: hint, exit: exitUsage}
}

// ioEnv is the process environment of one run.
type ioEnv struct {
	stdin               io.Reader
	stdout, stderr      io.Writer
	stdinTTY, stdoutTTY bool
	readPassword        func() ([]byte, error) // terminal input without echo
}

// app is one run of the command line.
type app struct {
	ioEnv
	ctx   context.Context
	json  bool // JSON mode: --json, or stdout is not a terminal
	inv   *invocation
	in    *bufio.Reader
	exit  int // exit code set by a command that printed its data
	warns int
}

func run(ctx context.Context, args []string, e ioEnv) int {
	a := &app{ioEnv: e, ctx: ctx, json: !e.stdoutTTY, in: bufio.NewReader(e.stdin)}
	inv, err := parse(args)
	if err != nil {
		// The flags did not parse; an explicit request for JSON still
		// counts for the error object.
		a.json = a.json || wantsJSON(args)
		if len(args) > 0 && args[0] == "gui" && consoleAlone() {
			alertError(err) // launcher or shortcut: nobody reads stderr
		}
		return a.fail(err)
	}
	a.inv = inv
	a.json = !e.stdoutTTY || inv.flag("json")
	switch {
	case inv.version:
		a.emit(map[string]string{"version": archive.Version}, func(w io.Writer) {
			fmt.Fprintln(w, "rechnungsarchiv "+archive.Version)
		})
		return exitOK
	case inv.help || inv.cmd == nil:
		text := helpText(inv.cmd)
		a.emit(map[string]string{"help": text}, func(w io.Writer) { io.WriteString(w, text) })
		return exitOK
	}
	if err := inv.cmd.run(a, inv); err != nil {
		return a.fail(err)
	}
	return a.exit
}

// wantsJSON scans the raw arguments for --json or --json=true, for an
// error before the flags parsed. The last occurrence wins.
func wantsJSON(args []string) bool {
	on := false
	for _, s := range args {
		switch s {
		case "--":
			return on
		case "--json", "-json", "--json=true", "-json=true":
			on = true
		case "--json=false", "-json=false":
			on = false
		}
	}
	return on
}

// interactive reports whether a person can be asked: stdin is a terminal
// and the run is not in JSON mode. Agents and scripts never are.
func (a *app) interactive() bool { return a.stdinTTY && !a.json }

// --- output ---

// emit prints data as JSON in JSON mode, else calls human.
func (a *app) emit(data any, human func(w io.Writer)) {
	if a.json {
		writeJSON(a.stdout, data)
		return
	}
	human(a.stdout)
}

// warn prints a warning on stderr.
func (a *app) warn(msg string) {
	a.warns++
	if a.json {
		writeJSON(a.stderr, map[string]string{"warning": msg})
		return
	}
	fmt.Fprintln(a.stderr, "Hinweis: "+msg)
}

// fail prints err on stderr and returns its exit code.
func (a *app) fail(err error) int {
	ce := classify(err)
	if a.json {
		writeJSON(a.stderr, map[string]any{"error": ce})
	} else {
		fmt.Fprintln(a.stderr, "Fehler: "+ce.Message)
		if ce.Hint != "" {
			fmt.Fprintln(a.stderr, "Hinweis: "+ce.Hint)
		}
	}
	return ce.exit
}

// report prints a non-fatal error object for a command whose data was
// already printed (e.g. archive committed, but the timestamp failed).
func (a *app) report(ce *cliError) {
	a.fail(ce)
	a.exit = ce.exit
}

// writeJSON writes v as one line of ASCII-only JSON: every non-ASCII
// character is escaped, so Windows PowerShell 5.1 reads it intact.
func writeJSON(w io.Writer, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		fmt.Fprintf(&buf, `{"error":{"code":"ERROR","message":%q}}`+"\n", err.Error())
	}
	w.Write(asciiJSON(buf.Bytes()))
}

func asciiJSON(b []byte) []byte {
	var out bytes.Buffer
	for _, r := range string(b) {
		if r < 0x80 {
			out.WriteRune(r)
			continue
		}
		if r > 0xffff {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&out, `\u%04x\u%04x`, r1, r2)
			continue
		}
		fmt.Fprintf(&out, `\u%04x`, r)
	}
	return out.Bytes()
}

// classify maps an error of the core to the error object and exit code.
func classify(err error) *cliError {
	var ce *cliError
	var le *archive.LockedError
	switch {
	case errors.As(err, &ce):
		return ce
	case errors.As(err, &le):
		return &cliError{Code: "LOCKED", Message: le.Error(), exit: exitLocked,
			Hint:    "Warten, bis der andere PC fertig ist. Nur wenn die Sperre nach einem Absturz hängt und der Benutzer zugestimmt hat: denselben Befehl mit --take-over --yes wiederholen.",
			Details: le.LockInfo}
	case errors.Is(err, archive.ErrNoArchive):
		return &cliError{Code: "NO_ARCHIVE", Message: err.Error(), exit: exitConfig,
			Hint: `Im Archivordner ausführen, --archiv "<ordner>" angeben oder mit rechnungsarchiv init "<ordner>" --firma "<name>" ein Archiv anlegen.`}
	case errors.Is(err, archive.ErrBadConfig):
		return &cliError{Code: "CONFIG", Message: err.Error(), exit: exitConfig, Hint: "rechnungsarchiv.json nicht von Hand bearbeiten; aus der Sicherung wiederherstellen."}
	case errors.Is(err, archive.ErrExists):
		return &cliError{Code: "ARCHIVE_EXISTS", Message: err.Error(), exit: exitError, Hint: "rechnungsarchiv status --json"}
	case errors.Is(err, archive.ErrNeedBelegjahr):
		return &cliError{Code: "BELEGJAHR_MISSING", Message: err.Error(), exit: exitUsage, Hint: "--belegjahr <Jahr> angeben."}
	case errors.Is(err, archive.ErrRetention):
		return &cliError{Code: "USAGE", Message: err.Error(), exit: exitUsage, Hint: "--aufbewahrung 7, 10, 12 oder 22"}
	case errors.Is(err, creds.ErrUnsupported):
		return &cliError{Code: "CONFIG", Message: err.Error(), exit: exitConfig}
	case errors.Is(err, fs.ErrNotExist):
		return &cliError{Code: "NOT_FOUND", Message: notFoundMsg(err), exit: exitUsage, Hint: "Pfad prüfen; Pfade mit Leerzeichen in Anführungszeichen setzen."}
	case errors.Is(err, context.Canceled):
		return &cliError{Code: "ERROR", Message: "abgebrochen", exit: exitError}
	}
	return &cliError{Code: "ERROR", Message: err.Error(), exit: exitError}
}

func notFoundMsg(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return "nicht gefunden: " + pe.Path
	}
	return err.Error()
}

// --- input ---

// confirm asks a person on the terminal. Callers check interactive first.
func (a *app) confirm(question string) bool {
	fmt.Fprint(a.stderr, question+" [j/N] ")
	line, _ := a.in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "j", "ja", "y", "yes":
		return true
	}
	return false
}

// consentHints say how to obtain consent for each action; export has no
// --dry-run, so its hint names what to show instead.
var consentHints = map[string]string{
	"archive": "Erst rechnungsarchiv archive --dry-run ausführen und den Plan dem Benutzer zeigen; --yes nur übergeben, wenn er genau dieser Aktion zugestimmt hat.",
	"export":  "Belegjahre und Ziel (details) dem Benutzer zeigen; export hat keinen Probelauf. --yes nur übergeben, wenn er genau dieser Aktion zugestimmt hat.",
}

// consent enforces ADR-0008: without --yes, an interactive run asks, any
// other run exits 8 and changes nothing.
func (a *app) consent(action, question string, details any) error {
	if a.inv.flag("yes") {
		return nil
	}
	if a.interactive() && a.confirm(question) {
		return nil
	}
	return &cliError{Code: "CONSENT_REQUIRED", exit: exitConsent, Details: details,
		Message: action + " braucht die Zustimmung des Benutzers (--yes); es wurde nichts geändert.",
		Hint:    consentHints[action]}
}

// maxSecret bounds what is read from stdin as a password.
const maxSecret = 4096

// secret reads a password: hidden from the terminal in an interactive run,
// else the first line of stdin. It is never taken from an argument.
func (a *app) secret(what string) (string, error) {
	if a.stdinTTY {
		if a.json {
			return "", &cliError{Code: "SECRET_MISSING", exit: exitUsage,
				Message: what + " fehlt: im JSON-Modus wird nicht nachgefragt.",
				Hint:    "Das Passwort über die Standardeingabe übergeben (Pipe), nie als Argument."}
		}
		fmt.Fprint(a.stderr, what+" (Eingabe wird nicht angezeigt): ")
		b, err := a.readPassword()
		fmt.Fprintln(a.stderr)
		if err != nil {
			return "", err
		}
		return checkSecret(what, string(b))
	}
	var b []byte
	for len(b) <= maxSecret {
		c, err := a.in.ReadByte()
		if err != nil || c == '\n' {
			break
		}
		b = append(b, c)
	}
	if len(b) > maxSecret {
		return "", &cliError{Code: "USAGE", exit: exitUsage, Message: what + " ist zu lang."}
	}
	s := strings.TrimSuffix(string(b), "\r")
	s = strings.TrimPrefix(s, "\xef\xbb\xbf") // PowerShell may send a BOM
	return checkSecret(what, s)
}

func checkSecret(what, s string) (string, error) {
	if s == "" {
		return "", &cliError{Code: "SECRET_MISSING", exit: exitUsage, Message: what + " fehlt.",
			Hint: "Das Passwort als erste Zeile über die Standardeingabe übergeben, nie als Argument."}
	}
	return s, nil
}

// --- archive location ---

// archiveDir finds the archive: --archiv, else the current folder if it
// contains rechnungsarchiv.json, else the remembered default archive.
func (a *app) archiveDir() (dir, source string, err error) {
	if d := a.inv.str("archiv"); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return "", "", err
		}
		if !isArchive(abs) {
			return "", "", &cliError{Code: "NO_ARCHIVE", exit: exitConfig,
				Message: "kein Archiv: " + abs + " enthält keine rechnungsarchiv.json",
				Hint:    `Pfad prüfen oder mit rechnungsarchiv init "<ordner>" --firma "<name>" ein Archiv anlegen.`}
		}
		return abs, "flag", nil
	}
	if cwd, err := os.Getwd(); err == nil && isArchive(cwd) {
		return cwd, "cwd", nil
	}
	if d := archive.DefaultDir(); d != "" {
		return d, "default", nil
	}
	return "", "", archive.ErrNoArchive
}

func isArchive(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "rechnungsarchiv.json"))
	return err == nil && fi.Mode().IsRegular()
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	ap, err1 := filepath.Abs(p)
	ad, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ad, ap)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// --- argument parsing ---

type flagKind int

const (
	boolFlag flagKind = iota
	stringFlag
	intFlag
)

func (k flagKind) String() string {
	switch k {
	case stringFlag:
		return "string"
	case intFlag:
		return "integer"
	}
	return "boolean"
}

type flagSpec struct {
	Name string
	Kind flagKind
	Arg  string // placeholder for the help, e.g. "<name>"
	Help string // German
}

// globalFlags are accepted by every command.
var globalFlags = []flagSpec{
	{Name: "archiv", Kind: stringFlag, Arg: "<ordner>", Help: "Archivordner (Standard: aktueller Ordner, wenn er rechnungsarchiv.json enthält, sonst das Standardarchiv)"},
	{Name: "json", Kind: boolFlag, Help: "nur JSON auf die Standardausgabe (automatisch, wenn die Ausgabe kein Terminal ist)"},
	{Name: "help", Kind: boolFlag, Help: "Hilfe zu diesem Befehl (auch -h)"},
}

// secretFlags are refused with a hint: secrets never go into arguments.
var secretFlags = map[string]bool{"password": true, "passwort": true, "pass": true, "pw": true, "secret": true, "pin": true, "p12-password": true, "kennwort": true}

type invocation struct {
	cmd     *command
	args    []string
	values  map[string]string
	help    bool
	version bool
}

func (i *invocation) flag(name string) bool {
	_, ok := i.values[name]
	return ok && i.values[name] != "false"
}
func (i *invocation) str(name string) string { return i.values[name] }
func (i *invocation) num(name string) int {
	n, _ := strconv.Atoi(i.values[name])
	return n
}

// parse reads the command line. Error messages never repeat the value of
// an unknown flag or an unexpected argument: it could be a password.
func parse(args []string) (*invocation, error) {
	inv := &invocation{values: map[string]string{}}
	var words []string
	var rest []string // tokens after the command word(s)
	i := 0
	// Before the command: global flags only.
	for ; i < len(args); i++ {
		s := args[i]
		if !strings.HasPrefix(s, "-") || s == "-" {
			break
		}
		switch s {
		case "--version":
			inv.version = true
			continue
		case "-h", "--help":
			inv.help = true
			continue
		}
		name, val, hasVal := splitFlag(s)
		spec := lookup(globalFlags, name)
		if spec == nil {
			return nil, unknownFlag(name)
		}
		if spec.Kind != boolFlag && !hasVal {
			if i+1 >= len(args) {
				return nil, usageErr("--"+name+" braucht einen Wert", "")
			}
			i++
			val = args[i]
		}
		if err := inv.set(spec, val, hasVal || spec.Kind != boolFlag); err != nil {
			return nil, err
		}
	}
	if i < len(args) {
		words = append(words, args[i])
		i++
		if i < len(args) && hasSub(words[0]) && !strings.HasPrefix(args[i], "-") {
			words = append(words, args[i])
			i++
		}
		rest = args[i:]
	}
	if len(words) == 0 {
		return inv, nil
	}
	if words[0] == "help" {
		inv.help = true
		// Global flags (--json …) may stand anywhere; the remaining words
		// name the command.
		var topic []string
		for j := 0; j < len(rest); j++ {
			s := rest[j]
			if !strings.HasPrefix(s, "-") || s == "-" {
				topic = append(topic, s)
				continue
			}
			if s == "-h" || s == "--help" {
				continue
			}
			name, val, hasVal := splitFlag(s)
			spec := lookup(globalFlags, name)
			if spec == nil {
				return nil, unknownFlag(name)
			}
			if spec.Kind != boolFlag && !hasVal {
				if j+1 >= len(rest) {
					return nil, usageErr("--"+name+" braucht einen Wert", "")
				}
				j++
				val = rest[j]
			}
			if err := inv.set(spec, val, hasVal || spec.Kind != boolFlag); err != nil {
				return nil, err
			}
		}
		inv.cmd = find(strings.Join(topic, " "))
		if len(topic) > 0 && inv.cmd == nil {
			inv.cmd = find(topic[0])
		}
		return inv, nil
	}
	inv.cmd = find(strings.Join(words, " "))
	if inv.cmd == nil {
		if len(words) == 2 && find(words[0]) != nil {
			inv.cmd = find(words[0]) // "provider" + an argument
			rest = append([]string{words[1]}, rest...)
		} else if hasSub(words[0]) {
			if len(words) == 1 {
				inv.cmd = find(words[0])
				inv.help = true
				return inv, nil
			}
			return nil, usageErr("unbekannter Befehl: "+words[0]+" <…>", "rechnungsarchiv "+words[0]+" --help")
		} else {
			return nil, usageErr("unbekannter Befehl: "+safeWord(words[0]), "")
		}
	}
	flags := append(append([]flagSpec{}, globalFlags...), inv.cmd.Flags...)
	onlyArgs := false
	for j := 0; j < len(rest); j++ {
		s := rest[j]
		if onlyArgs || !strings.HasPrefix(s, "-") || s == "-" {
			inv.args = append(inv.args, s)
			continue
		}
		if s == "--" {
			onlyArgs = true
			continue
		}
		if s == "-h" || s == "--help" {
			inv.help = true
			continue
		}
		if s == "-y" {
			s = "--yes"
		}
		name, val, hasVal := splitFlag(s)
		spec := lookup(flags, name)
		if spec == nil {
			return nil, unknownFlag(name)
		}
		if spec.Kind != boolFlag && !hasVal {
			if j+1 >= len(rest) {
				return nil, usageErr("--"+name+" braucht einen Wert", "rechnungsarchiv "+inv.cmd.Name+" --help")
			}
			j++
			val = rest[j]
		}
		if err := inv.set(spec, val, hasVal || spec.Kind != boolFlag); err != nil {
			return nil, err
		}
	}
	if inv.help {
		return inv, nil
	}
	if len(inv.args) < inv.cmd.MinArgs {
		return nil, usageErr(inv.cmd.Name+": Argument fehlt: "+inv.cmd.Args, "rechnungsarchiv "+inv.cmd.Name+" --help")
	}
	if len(inv.args) > inv.cmd.MaxArgs {
		// Do not repeat the argument: it could be a password.
		return nil, usageErr(fmt.Sprintf("%s: zu viele Argumente (%d erwartet, %d erhalten)", inv.cmd.Name, inv.cmd.MaxArgs, len(inv.args)),
			"Pfade mit Leerzeichen in Anführungszeichen setzen. Passwörter nie als Argument übergeben. rechnungsarchiv "+inv.cmd.Name+" --help")
	}
	return inv, nil
}

func (inv *invocation) set(spec *flagSpec, val string, hasVal bool) error {
	if _, dup := inv.values[spec.Name]; dup {
		return usageErr("--"+spec.Name+" doppelt angegeben", "")
	}
	switch spec.Kind {
	case boolFlag:
		if !hasVal {
			val = "true"
		}
		if val != "true" && val != "false" {
			return usageErr("--"+spec.Name+" erwartet keinen Wert", "")
		}
	case intFlag:
		if _, err := strconv.Atoi(val); err != nil {
			return usageErr("--"+spec.Name+" erwartet eine ganze Zahl", "")
		}
	case stringFlag:
		if val == "" {
			return usageErr("--"+spec.Name+" braucht einen Wert", "")
		}
	}
	inv.values[spec.Name] = val
	return nil
}

func splitFlag(s string) (name, val string, hasVal bool) {
	s = strings.TrimLeft(s, "-")
	name, val, hasVal = strings.Cut(s, "=")
	return name, val, hasVal
}

func lookup(specs []flagSpec, name string) *flagSpec {
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	return nil
}

func unknownFlag(name string) error {
	if secretFlags[strings.ToLower(name)] {
		return &cliError{Code: "USAGE", exit: exitUsage,
			Message: "Passwörter werden nie als Argument übergeben (--" + safeWord(name) + ").",
			Hint:    "provider set liest das Passwort von der Standardeingabe; gespeichert wird es im Anmeldeinformationsspeicher von Windows."}
	}
	return usageErr("unbekannte Option: --"+safeWord(name), "")
}

// safeWord returns s for an error message if it looks like a flag name or
// a command word (lowercase letters, digits, dashes); anything else may be
// a mistyped secret and is not repeated.
func safeWord(s string) string {
	if len(s) > 24 || s == "" {
		return "…"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "…"
		}
	}
	return s
}
