package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// conforms checks v (decoded JSON) against s, the JSON Schema subset that
// outSchema emits: type, const, enum, format date-time, properties,
// required, additionalProperties, items, oneOf (exactly one branch).
func conforms(s map[string]any, v any, at string) error {
	if alts, ok := s["oneOf"].([]any); ok {
		var errs []string
		n := 0
		for _, a := range alts {
			if err := conforms(a.(map[string]any), v, at); err != nil {
				errs = append(errs, err.Error())
			} else {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("%s: matches %d of %d oneOf branches (%s)", at, n, len(alts), strings.Join(errs, "; "))
		}
		return nil
	}
	if c, ok := s["const"]; ok && v != c {
		return fmt.Errorf("%s: %v, want const %v", at, v, c)
	}
	if e, ok := s["enum"].([]any); ok && !slices.Contains(e, v) {
		return fmt.Errorf("%s: %v not in enum", at, v)
	}
	if typ, ok := s["type"]; ok {
		types, _ := typ.([]any)
		if str, ok := typ.(string); ok {
			types = []any{str}
		}
		if !slices.ContainsFunc(types, func(x any) bool { return isJSONType(x.(string), v) }) {
			return fmt.Errorf("%s: %T %v is not of type %v", at, v, v, types)
		}
	}
	if s["format"] == "date-time" {
		if str, ok := v.(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
				return fmt.Errorf("%s: %q is not a date-time", at, str)
			}
		}
	}
	switch v := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		req, _ := s["required"].([]any)
		for _, k := range req {
			if _, ok := v[k.(string)]; !ok {
				return fmt.Errorf("%s: required key %q missing", at, k)
			}
		}
		for k, x := range v {
			sub, ok := props[k].(map[string]any)
			if !ok {
				switch ap := s["additionalProperties"].(type) {
				case bool:
					if !ap {
						return fmt.Errorf("%s: key %q not in the schema", at, k)
					}
					continue
				case map[string]any:
					sub = ap
				default:
					continue
				}
			}
			if err := conforms(sub, x, at+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, x := range v {
				if err := conforms(items, x, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isJSONType(t string, v any) bool {
	switch t {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return false
}

// The validator itself: it must reject what the schema forbids, or the
// drift check above passes anything.
func TestConformsRejects(t *testing.T) {
	plan := outSchema(planOut{})
	arch := outSchema(oneOf{planOut{DryRun: true}, archiveOut{}})
	var good map[string]any
	data, _ := json.Marshal(planOut{DryRun: true, Files: []planFile{}, Skipped: []string{}, Known: []string{}, Years: []int{}, Notes: []string{}})
	json.Unmarshal(data, &good)
	// Round-trip through JSON: the test sees the schema as published.
	var pub, pubArch map[string]any
	b, _ := json.Marshal(plan)
	json.Unmarshal(b, &pub)
	b, _ = json.Marshal(arch)
	json.Unmarshal(b, &pubArch)
	if err := conforms(pubArch, any(good), "plan"); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(m map[string]any){
		"extra key":    func(m map[string]any) { m["neu"] = 1.0 },
		"missing key":  func(m map[string]any) { delete(m, "count") },
		"wrong type":   func(m map[string]any) { m["count"] = "9" },
		"fraction":     func(m map[string]any) { m["count"] = 1.5 },
		"null array":   func(m map[string]any) { m["files"] = nil },
		"null string":  func(m map[string]any) { m["name"] = nil },
		"dry_run":      func(m map[string]any) { m["dry_run"] = false },
		"nested":       func(m map[string]any) { m["files"] = []any{map[string]any{"path": "a", "size": 1.0}} },
		"not an array": func(m map[string]any) { m["years"] = map[string]any{} },
	} {
		m := map[string]any{}
		for k, v := range good {
			m[k] = v
		}
		mutate(m)
		if conforms(pubArch, any(m), "plan") == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// provider: a pointer without omitempty is null, never absent.
	if good["provider"] != nil || conforms(pub, any(good), "plan") != nil {
		t.Fatal("null provider refused")
	}
}

// The stderr lines as the CLI writes them match error_document and
// warning_document as schema publishes them.
func TestStderrDocuments(t *testing.T) {
	var doc map[string]any
	var buf strings.Builder
	writeJSON(&buf, schemaDoc())
	if err := json.Unmarshal([]byte(buf.String()), &doc); err != nil {
		t.Fatal(err)
	}
	var errb strings.Builder
	a := &app{ioEnv: ioEnv{stderr: &errb}, json: true}
	a.warn("Hinweis mit Umlaut: Prüfwert")
	a.fail(&cliError{Code: "LOCKED", exit: exitLocked, Message: "gesperrt", Hint: "warten", Details: map[string]any{"host": "pc"}})
	a.fail(errors.New("irgendwas"))
	lines := strings.Split(strings.TrimSpace(errb.String()), "\n")
	for i, want := range []string{"warning_document", "error_document", "error_document"} {
		var v any
		if err := json.Unmarshal([]byte(lines[i]), &v); err != nil {
			t.Fatal(err)
		}
		if err := conforms(doc[want].(map[string]any), v, want); err != nil {
			t.Errorf("%v: %s", err, lines[i])
		}
	}
	if conforms(doc["error_document"].(map[string]any), map[string]any{"error": map[string]any{"code": "NEU", "message": "x"}}, "e") == nil {
		t.Error("unknown error code accepted")
	}
}

// Shapes the walker does not model panic instead of publishing a wrong
// schema; TestEffectsAndNetworkDeclared builds every command's schema.
type cycleNode struct{ *cycleNode }
type cyclePtr *cyclePtr
type cycleTree map[string]cycleTree
type embA struct{ ID int }
type embB struct{ ID int }
type embConflict struct {
	embA
	embB
}
type embShadow struct {
	embA
	ID string
}

func TestOutSchemaRefusesUnmodelled(t *testing.T) {
	for name, v := range map[string]any{
		"embedded cycle": cycleNode{},
		"pointer cycle":  cyclePtr(nil),
		"map cycle":      cycleTree{},
		"slice cycle": struct {
			Kids []cycleNode `json:"kids"`
		}{},
		"sibling conflict": embConflict{},
		"shadowed field":   embShadow{},
		",string tag": struct {
			N *int `json:"n,string"`
		}{},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			outSchema(v)
		}()
	}

	// omitempty drops only the pointer's own null: a pointer to a nil
	// slice still marshals null.
	type ptrSlice struct {
		S *[]string `json:"s,omitempty"`
	}
	var nilSlice []string
	data, _ := json.Marshal(ptrSlice{S: &nilSlice})
	var v, s any
	json.Unmarshal(data, &v)
	b, _ := json.Marshal(outSchema(ptrSlice{}))
	json.Unmarshal(b, &s)
	if err := conforms(s.(map[string]any), v, "ptrSlice"); err != nil {
		t.Errorf("%v: %s", err, data)
	}
	if conforms(s.(map[string]any), map[string]any{"s": "x"}, "ptrSlice") == nil {
		t.Error("string accepted for *[]string")
	}
}

// published returns the schema document the binary prints and its
// commands[].output by command name.
func published(t *testing.T, w *world) (map[string]any, map[string]map[string]any) {
	t.Helper()
	sd := w.run("", "schema").want(t, 0)
	outputs := map[string]map[string]any{}
	for _, c := range sd["commands"].([]any) {
		c := c.(map[string]any)
		if o, ok := c["output"].(map[string]any); ok {
			outputs[c["name"].(string)] = o
		}
	}
	return sd, outputs
}

// schemaChecker reads the published schema once and returns a check of
// one run: its exit code is code; with doc its stdout is one document that
// matches commands[].output of name, without doc stdout is empty; every
// stderr line matches error_document or warning_document. It returns r.
// all: at the end of the test, every command with an output and the error
// document must have been checked.
func schemaChecker(t *testing.T, w *world, all bool) func(name string, r result, code int, doc bool) result {
	sd, outputs := published(t, w)
	errDoc := sd["error_document"].(map[string]any)
	warnDoc := sd["warning_document"].(map[string]any)
	seen := map[string]bool{}
	t.Cleanup(func() {
		if !all {
			return
		}
		for _, n := range []string{"status", "init", "archive", "stamp", "verify", "export", "protocol", "providers", "provider set", "provider show", "error"} {
			if !seen[n] && !t.Failed() {
				t.Errorf("schema of %s never checked", n)
			}
		}
	})
	return func(name string, r result, code int, doc bool) result {
		t.Helper()
		s, ok := outputs[name]
		if !ok {
			t.Fatalf("schema has no output for %s", name)
		}
		if r.code != code {
			t.Fatalf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", name, r.code, code, r.stdout, r.stderr)
		}
		switch empty := strings.TrimSpace(r.stdout) == ""; {
		case doc && empty:
			t.Fatalf("%s (exit %d): no stdout document\nstderr: %s", name, code, r.stderr)
		case !doc && !empty:
			t.Fatalf("%s (exit %d): stdout must be empty: %s", name, code, r.stdout)
		case doc:
			var v any
			if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
				t.Fatalf("%s: stdout is not JSON: %v", name, err)
			}
			if err := conforms(s, v, name); err != nil {
				t.Fatalf("%s: %v\n%s", name, err, r.stdout)
			}
			seen[name] = true
		}
		for _, line := range strings.Split(strings.TrimSpace(r.stderr), "\n") {
			if line == "" {
				continue
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Fatalf("%s: stderr line is not JSON: %q", name, line)
			}
			kind, s := "error", errDoc
			if _, ok := v["warning"]; ok {
				kind, s = "warning", warnDoc
			}
			if err := conforms(s, any(v), name+" stderr"); err != nil {
				t.Fatalf("%s: %v\n%s", name, err, line)
			}
			seen[kind] = true
		}
		return r
	}
}

// Empty results keep their lists: deposits, trust_lists and outcomes are
// [] (never null), as the schema says.
func TestEmptyResultsMatchSchema(t *testing.T) {
	w := newWorld(t)
	check := schemaChecker(t, w, false)
	dir := filepath.Join(t.TempDir(), "Archiv")
	check("init", w.run("", "init", dir, "--firma", "Leer GmbH"), 0, true)
	st := check("status", w.run("", "status"), 0, true).want(t, 0)
	rep := check("verify", w.run("", "verify"), 0, true).want(t, 0)
	s := check("stamp", w.run("", "stamp"), 0, true).want(t, 0)
	for name, v := range map[string]any{"status.pending": st["pending"], "verify.deposits": rep["deposits"], "verify.trust_lists": rep["trust_lists"], "stamp.outcomes": s["outcomes"]} {
		if l, ok := v.([]any); !ok || (name != "verify.trust_lists" && len(l) != 0) {
			t.Errorf("%s: %v", name, v)
		}
	}
	// The published schema must refuse null for every list the command
	// guarantees: a widened schema (annotation lost) fails here.
	_, outputs := published(t, w)
	for _, c := range []struct {
		cmd  string
		doc  map[string]any
		keys []string
	}{
		{"status", st, []string{"pending", "unreadable", "trust_lists", "next"}},
		{"verify", rep, []string{"deposits", "trust_lists"}},
		{"stamp", s, []string{"outcomes"}},
	} {
		for _, k := range c.keys {
			m := map[string]any{}
			for kk, v := range c.doc {
				m[kk] = v
			}
			m[k] = nil
			if conforms(outputs[c.cmd], any(m), c.cmd) == nil {
				t.Errorf("%s: schema accepts null for %s", c.cmd, k)
			}
		}
	}
	check("archive", w.run("", "archive", demo(demoIncoming)), 8, false)
}
