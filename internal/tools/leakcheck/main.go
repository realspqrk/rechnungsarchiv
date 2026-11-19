// Command leakcheck is a release gate: it fails when private or secret data
// is in the repository (every blob reachable from any branch or tag, the
// index, and every file git would commit now) or in release artifacts.
//
//	go run ./internal/tools/leakcheck                       # repository
//	go run ./internal/tools/leakcheck -artifacts dist\release\*.exe …
//
// Rules: private keys, access tokens, Windows user paths, e-mail addresses
// outside the allowlist, phone numbers, password assignments outside tests,
// and file names of key stores, signing metadata and local settings. Text
// in UTF-16 (with BOM, or BOM-less little endian) is decoded first.
//
// Private strings that must never be published (an account name, a private
// address) are listed one per line in a file outside the repository:
// <UserConfigDir>/Rechnungsarchiv/leakcheck-private.txt (Windows:
// %APPDATA%\Rechnungsarchiv\leakcheck-private.txt), UTF-8 or UTF-16. Lines
// starting with # are comments. Matching ignores ASCII case.
//
// Exit status: 0 clean, 1 findings, 2 error.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type rule struct {
	id string
	re *regexp.Regexp
	// skip: path prefixes (or "_test.go" suffix) where the rule does not
	// apply, for public or test-only data.
	skip []string
	// allow: a match is fine if this returns true.
	allow func(match string) bool
}

// publicData: EU trusted lists and their test copies name the contacts and
// certificates of trust service providers (public by law).
var publicData = []string{"testdata/trust/", "internal/trust/testdata/", "internal/trust/pins/"}

// fileScale: image names with a scale suffix ("logo@2x.png") look like
// e-mail addresses but are file names.
var fileScale = regexp.MustCompile(`(?i)@\d+(\.\d+)?x\.(png|jpe?g|gif|webp|svg|avif)$`)

var allowedEmail = regexp.MustCompile(`(?i)^(office@spqrk\.net|sales@globaltrust\.eu|noreply@anthropic\.com|noreply@github\.com|[^@]+@users\.noreply\.github\.com|[^@]+@example\.(com|org|net)|[^@]+@[^@]+\.(invalid|example|test))$`)

var contentRules = []rule{
	{id: "private-key", re: regexp.MustCompile(`-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY( BLOCK)?-----`)},
	{id: "github-token", re: regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{id: "aws-key", re: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b|(?i)\baws_(secret_access_key|session_token)\s*=\s*[A-Za-z0-9/+=]{20,}`)},
	{id: "azure-key", re: regexp.MustCompile(`(?i)(AccountKey|SharedAccessKey|client_secret)["']?\s*[=:]\s*["']?[A-Za-z0-9+/=_~.-]{30,}`)},
	{id: "api-key", re: regexp.MustCompile(`\b(sk-(ant-|proj-)?[A-Za-z0-9_-]{32,}|AIza[0-9A-Za-z_-]{35}|xox[abprs]-[A-Za-z0-9-]{10,})\b`)},
	{id: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{id: "user-path", re: regexp.MustCompile(`(?i)\b[a-z]:(\\\\?|/)users(\\\\?|/)[a-z0-9._-]+`), allow: func(m string) bool {
		name := strings.ToLower(m[strings.LastIndexAny(m, `\/`)+1:])
		return name == "public" || name == "default" || name == "runneradmin"
	}},
	{id: "email", re: regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`), skip: publicData,
		allow: func(m string) bool { return allowedEmail.MatchString(m) || fileScale.MatchString(m) }},
	{id: "phone", re: regexp.MustCompile(`(\+|\b00)(43|49|41)[ (]*\d[\d /()-]{6,}\d`), skip: publicData},
	// The key may end in the word (client_secret, access_token) and be
	// quoted (JSON).
	{id: "password", re: regexp.MustCompile(`(?i)\b([a-z0-9]+_)*(password|passwort|passwd|secret|api_?key|token)["']?\s*[:=]\s*["'][^"'\s]{8,}["']`),
		skip: []string{"_test.go", "internal/testenv/"}},
}

var nameRule = regexp.MustCompile(`(?i)(^|/)(\.env(\..*)?|\.aws/credentials|metadata\.json|id_(rsa|dsa|ecdsa|ed25519)|settings\.local\.json|credentials[^/]*\.json|[^/]*\.(p12|pfx|key|jks|keystore|ppk|kdbx|pem))$`)

type finding struct{ rule, where, excerpt string }

func main() {
	artifacts := flag.Bool("artifacts", false, "scan the given release files (and ZIPs inside them) instead of the repository")
	flag.Parse()
	private, err := privateStrings()
	if err != nil {
		fail(err)
	}
	// The private strings never appear in the output, also not inside a
	// path (a file named after the account) or an error message.
	hide := redactor(private)
	var found []finding
	if *artifacts {
		found, err = scanArtifacts(flag.Args(), private)
	} else {
		found, err = scanRepo(private)
	}
	if err != nil {
		fail(errors.New(hide(err.Error())))
	}
	for _, f := range found {
		fmt.Printf("LEAK %-12s %s  %s\n", f.rule, hide(f.where), hide(f.excerpt))
	}
	if !*artifacts {
		if ids, err := identities(); err == nil {
			fmt.Println("info: identities in commit and tag metadata (public with every push):", hide(strings.Join(ids, ", ")))
		}
	}
	if len(found) > 0 {
		fmt.Printf("leakcheck: %d finding(s). Nothing may be released until each is removed (and, if pushed, rotated and purged from history).\n", len(found))
		os.Exit(1)
	}
	fmt.Println("leakcheck: clean")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "leakcheck:", err)
	os.Exit(2)
}

func privateStrings() ([]string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil
	}
	file := filepath.Join(dir, "Rechnungsarchiv", "leakcheck-private.txt")
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out, err := parsePrivate(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	return out, nil
}

// redactor replaces every private string (ignoring case) with "‹privat›".
func redactor(private []string) func(string) string {
	if len(private) == 0 {
		return func(s string) string { return s }
	}
	var alts []string
	for _, p := range private {
		alts = append(alts, regexp.QuoteMeta(p))
	}
	re := regexp.MustCompile(`(?i)` + strings.Join(alts, "|"))
	return func(s string) string { return re.ReplaceAllString(s, "‹privat›") }
}

// parsePrivate reads the private list: UTF-8 (BOM optional) or UTF-16,
// LF or CRLF.
func parsePrivate(data []byte) ([]string, error) {
	text, ok := textOf(data)
	if !ok {
		return nil, errors.New("not a text file")
	}
	// A list saved in the ANSI code page (Set-Content in PowerShell 5.1)
	// would silently miss names with umlauts.
	// Damaged UTF-16 (cut off, lone surrogates) decodes to U+FFFD.
	if !utf8.Valid(text) || bytes.ContainsRune(text, utf8.RuneError) {
		return nil, errors.New("not UTF-8 or UTF-16: save it as UTF-8 (e.g. Set-Content -Encoding utf8)")
	}
	var out []string
	for _, l := range strings.Split(string(text), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, nil
}

// textOf returns data as UTF-8 text: a UTF-8 BOM is dropped, UTF-16 (BOM,
// or BOM-less little endian such as PowerShell output) is decoded. ok is
// false for binary data.
func textOf(data []byte) (text []byte, ok bool) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		text = data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		text = decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		text = decodeUTF16(data[2:], true)
	case looksUTF16LE(data):
		text = decodeUTF16(data, false)
	default:
		text = data
	}
	return text, bytes.IndexByte(text[:min(len(text), 8000)], 0) < 0
}

// looksUTF16LE: at least 90 % of the high bytes are zero (Latin text).
func looksUTF16LE(data []byte) bool {
	sample := data[:min(len(data), 8000)&^1]
	if len(sample) < 4 {
		return false
	}
	zeros := 0
	for i := 1; i < len(sample); i += 2 {
		if sample[i] == 0 {
			zeros++
		}
	}
	return zeros*10 >= len(sample)/2*9
}

func decodeUTF16(b []byte, bigEndian bool) []byte {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
	}
	out := []byte(string(utf16.Decode(u)))
	if len(b)%2 == 1 {
		// A cut-off code unit stays visible as U+FFFD (parsePrivate refuses it).
		out = utf8.AppendRune(out, utf8.RuneError)
	}
	return out
}

// --- repository ---------------------------------------------------------------

// blob: the paths a blob had anywhere (history and index), and where it was
// first seen.
type blob struct {
	origin string
	paths  []string
}

func scanRepo(privateStrs []string) ([]finding, error) {
	private := needleSet(privateStrs)
	blobs := map[string]*blob{}
	var order []string
	add := func(sha, path, origin string) {
		b := blobs[sha]
		if b == nil {
			b = &blob{origin: origin}
			blobs[sha] = b
			order = append(order, sha)
		}
		if path != "" && !slices.Contains(b.paths, path) {
			b.paths = append(b.paths, path)
		}
	}
	// History: every blob reachable from any ref ...
	list, err := git("rev-list", "--all", "--objects", "--filter=object:type=blob")
	if err != nil {
		return nil, err
	}
	for _, l := range strings.Split(strings.TrimSpace(string(list)), "\n") {
		if sha, path, _ := strings.Cut(l, " "); sha != "" {
			add(sha, path, "history")
		}
	}
	// ... with every path it had (rev-list names only one).
	raw, err := git("log", "--all", "--raw", "--no-abbrev", "--no-renames", "--root", "-m", "--format=", "-z")
	if err != nil {
		return nil, err
	}
	rawPaths(raw, func(sha, path string) { add(sha, path, "history") })
	// Index: what is staged, which may differ from the working copy.
	index, err := git("ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	for _, e := range strings.Split(strings.TrimRight(string(index), "\x00"), "\x00") {
		head, path, ok := strings.Cut(e, "\t") // <mode> <sha> <stage>\t<path>
		f := strings.Fields(head)
		if ok && len(f) == 3 && f[0] != "160000" { // not a submodule commit
			add(f[1], path, "index")
		}
	}
	var found []finding
	err = catFile(order, func(sha string, data []byte) {
		b := blobs[sha]
		paths := b.paths
		if len(paths) == 0 {
			paths = []string{"(no path)"}
		}
		found = append(found, scanBlob(paths, b.origin+" "+sha[:10], data, private)...)
	})
	if err != nil {
		return nil, err
	}
	// Working tree: everything git would commit now (tracked and new).
	tree, err := git("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(strings.TrimRight(string(tree), "\x00"), "\x00") {
		data, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue // deleted, not yet committed
		}
		if err != nil {
			return nil, err
		}
		found = append(found, scanBlob([]string{p}, "worktree", data, private)...)
	}
	// One line per rule, place and excerpt (a file unchanged across
	// commits is one blob anyway).
	slices.SortFunc(found, func(a, b finding) int { return strings.Compare(a.where+a.rule+a.excerpt, b.where+b.rule+b.excerpt) })
	return slices.CompactFunc(found, func(a, b finding) bool { return a == b }), nil
}

// rawPaths parses `git log --raw -z` output: records
// ":<mode> <mode> <sha> <sha> <status>" NUL "<path>" NUL, paths unquoted.
// Commits are separated by empty records or newlines before the colon.
func rawPaths(out []byte, fn func(sha, path string)) {
	tok := strings.Split(string(out), "\x00")
	for i := 0; i < len(tok); i++ {
		head := strings.TrimLeft(tok[i], "\n")
		if !strings.HasPrefix(head, ":") {
			continue
		}
		f := strings.Fields(head[1:])
		if len(f) < 5 || i+1 >= len(tok) {
			continue
		}
		i++ // the path, whatever it looks like
		for _, side := range [][2]string{{f[0], f[2]}, {f[1], f[3]}} {
			mode, sha := side[0], side[1]
			if mode != "160000" && strings.Trim(sha, "0") != "" {
				fn(sha, tok[i])
			}
		}
	}
}

// scanBlob checks one content under all paths it had: the name rule per
// path, a content rule unless every path is exempt from it.
func scanBlob(paths []string, origin string, data []byte, private []needle) []finding {
	var out []finding
	at := func(p string) string { return p + " (" + origin + ")" }
	for _, p := range paths {
		if nameRule.MatchString(p) && !skipped(p, publicData) {
			out = append(out, finding{"file-name", at(p), filepath.Base(p)})
		}
		// A private string in a file or folder name is published as well.
		for _, f := range privateHits(at(p), private, []byte(p)) {
			f.rule = "private-name"
			out = append(out, f)
		}
	}
	text, ok := textOf(data)
	if !ok {
		return append(out, privateHits(at(paths[0]), private, data)...) // binary
	}
	for _, r := range contentRules {
		i := slices.IndexFunc(paths, func(p string) bool { return !skipped(p, r.skip) })
		if i < 0 {
			continue
		}
		for _, m := range r.re.FindAll(text, -1) {
			if r.allow != nil && r.allow(string(m)) {
				continue
			}
			out = append(out, finding{r.id, at(paths[i]), redact(string(m))})
		}
	}
	// The raw bytes cover UTF-8 and UTF-16LE; decoded text adds UTF-16BE.
	if len(text) > 0 && &text[len(text)-1] == &data[len(data)-1] {
		text = nil // not decoded, a part of data
	}
	return append(out, privateHits(at(paths[0]), private, data, text)...)
}

func skipped(path string, skip []string) bool {
	for _, s := range skip {
		if strings.HasPrefix(path, s) || strings.HasPrefix(s, "_") && strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// redact keeps enough to find the place, never the whole secret.
func redact(m string) string {
	if len(m) <= 12 {
		return m[:min(len(m), 4)] + "…"
	}
	return m[:8] + "…" + strconv.Itoa(len(m)) + " chars"
}

func git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

// catFile streams the blobs through one `git cat-file --batch`.
func catFile(shas []string, fn func(sha string, data []byte)) error {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReaderSize(stdout, 1<<20)
	for range shas {
		head, err := r.ReadString('\n')
		if err != nil {
			return fmt.Errorf("git cat-file: %v", err)
		}
		f := strings.Fields(head) // <sha> <type> <size>
		if len(f) != 3 {
			return fmt.Errorf("git cat-file: %q", head)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return err
		}
		data := make([]byte, size+1) // content and the trailing newline
		if _, err := io.ReadFull(r, data); err != nil {
			return err
		}
		// rev-list also names annotated tags; their tagger is metadata
		// (identities()), not content.
		if f[1] == "blob" {
			fn(f[0], data[:size])
		}
	}
	return cmd.Wait()
}

// identities lists the author, committer and tagger e-mail addresses.
func identities() ([]string, error) {
	log, err := git("log", "--all", "--format=%ae%n%ce")
	if err != nil {
		return nil, err
	}
	tags, err := git("for-each-ref", "refs/tags", "--format=%(taggeremail:trim)")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, l := range strings.Fields(string(log) + " " + string(tags)) {
		if !slices.Contains(ids, l) {
			ids = append(ids, l)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// --- artifacts ----------------------------------------------------------------

// scanArtifacts looks for this PC's own traces (home folder, repository
// path) and the private strings in release files, also inside ZIPs (the
// Setup embeds the package as one), as text and UTF-16. The bare account
// name is no needle: short names ("user", "build") occur in any binary.
func scanArtifacts(files []string, private []string) ([]finding, error) {
	if len(files) == 0 {
		return nil, errors.New("-artifacts needs the release files")
	}
	needles := slices.Clone(private)
	if home, err := os.UserHomeDir(); err == nil {
		needles = append(needles, home, filepath.ToSlash(home))
	}
	if wd, err := os.Getwd(); err == nil {
		needles = append(needles, wd, filepath.ToSlash(wd))
	}
	needles = append(needles, "private key-----")
	set := needleSet(needles)
	var found []finding
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		// These must be ZIPs; a silent fallback to a raw scan would miss
		// every compressed member.
		if low := strings.ToLower(f); strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, "setup.exe") {
			if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
				return nil, fmt.Errorf("%s: cannot read the ZIP inside: %v", f, err)
			}
		}
		found = append(found, scanArtifact(f, data, set)...)
	}
	return found, nil
}

// isPackage: members that must open as ZIP (NuGet packages are ZIPs).
func isPackage(name string) bool {
	n := strings.ToLower(name[strings.LastIndex(name, "!")+1:])
	return strings.HasSuffix(n, ".zip") || strings.HasSuffix(n, ".nupkg")
}

func scanArtifact(name string, data []byte, needles []needle) []finding {
	out := privateHits(name, needles, data)
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// A package that cannot be opened is not checked: never clean.
		if isPackage(name) {
			out = append(out, finding{"unreadable", name, err.Error()})
		}
		return out
	}
	for _, m := range z.File {
		if m.FileInfo().IsDir() {
			continue
		}
		rc, err := m.Open()
		if err != nil {
			out = append(out, finding{"unreadable", name + "!" + m.Name, err.Error()})
			continue
		}
		inner, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			out = append(out, finding{"unreadable", name + "!" + m.Name, err.Error()})
			continue
		}
		out = append(out, scanArtifact(name+"!"+m.Name, inner, needles)...)
	}
	return out
}

// needle: one private string and its byte forms (as given and lower case,
// UTF-8 and UTF-16LE), ASCII-folded like the data they are searched in.
type needle struct {
	text  string
	forms [][]byte
}

func needleSet(strs []string) []needle {
	out := make([]needle, 0, len(strs))
	for _, s := range strs {
		n := needle{text: s}
		for _, v := range []string{s, strings.ToLower(s)} {
			for _, b := range [][]byte{[]byte(v), utf16le(v)} {
				b = asciiLower(b)
				if !slices.ContainsFunc(n.forms, func(f []byte) bool { return bytes.Equal(f, b) }) {
					n.forms = append(n.forms, b)
				}
			}
		}
		out = append(out, n)
	}
	return out
}

// privateHits reports each needle found in any of data.
func privateHits(where string, needles []needle, data ...[]byte) []finding {
	if len(needles) == 0 {
		return nil
	}
	var low [][]byte
	for _, d := range data {
		if d != nil {
			low = append(low, asciiLower(d))
		}
	}
	var out []finding
	for _, n := range needles {
		if slices.ContainsFunc(low, func(d []byte) bool {
			return slices.ContainsFunc(n.forms, func(f []byte) bool { return bytes.Contains(d, f) })
		}) {
			out = append(out, finding{"private", where, redact(n.text)})
		}
	}
	return out
}

// asciiLower folds only A-Z, so UTF-16 and binary data stay intact
// (bytes.ToLower would decode them as UTF-8).
func asciiLower(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}
