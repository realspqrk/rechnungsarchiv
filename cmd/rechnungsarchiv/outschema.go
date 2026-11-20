package main

import (
	"reflect"
	"slices"
	"strings"
	"time"
)

// The output schemas in `rechnungsarchiv schema` (commands[].output) are
// reflected from the very types the commands hand to encoding/json, so they
// follow its rules: json tag names, "-" and unexported fields left out,
// embedded structs flattened, omitempty/omitzero fields not required, a nil
// pointer without omitempty is null. A nil slice or map marshals as null,
// so such a field may be null unless the emitter guarantees a value (struct
// tag schema:"nonnull"). The result is a JSON Schema subset (draft 2020-12):
// type, properties, required, additionalProperties, items, oneOf, const,
// enum, format. Shapes it does not model panic (see walker).

// oneOf declares that a command prints one of several documents.
type oneOf []any

// selfDoc stands for the schema document itself (the schema command); it
// is not expanded.
type selfDoc struct{}

var timeType = reflect.TypeFor[time.Time]()

// outSchema returns the JSON Schema of the JSON that encoding/json writes
// for v's type. The top-level value is never null.
func outSchema(v any) map[string]any {
	switch v := v.(type) {
	case nil:
		return nil
	case oneOf:
		var alts []any
		for _, x := range v {
			alts = append(alts, outSchema(x))
		}
		return map[string]any{"oneOf": alts}
	case selfDoc:
		return map[string]any{"type": "object", "description": "this document (rechnungsarchiv schema); not expanded here"}
	}
	// The walker sees the type as given (a pointer cycle like type P *P
	// hits its guard); the top-level value is never null.
	s := nonNull((&walker{}).of(reflect.TypeOf(v)))
	if _, ok := v.(planOut); ok {
		// dry_run is always true: it tells the plan from archiveOut.
		s["properties"].(map[string]any)["dry_run"] = map[string]any{"type": "boolean", "const": true}
	}
	return s
}

// walker reflects one output type. It supports what the commands marshal
// and panics on what it does not model (an embedded field whose name
// collides, a type cycle, a ,string tag), so a new output type fails the
// tests that build every command's schema instead of publishing a wrong
// schema.
type walker struct{ stack []reflect.Type }

func (w *walker) push(t reflect.Type) {
	if slices.Contains(w.stack, t) {
		panic("outschema: type cycle through " + t.String() + "; the walker does not support recursive output types")
	}
	w.stack = append(w.stack, t)
}

func (w *walker) pop() { w.stack = w.stack[:len(w.stack)-1] }

func (w *walker) of(t reflect.Type) map[string]any {
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		w.push(t)
		defer w.pop()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return nullable(w.of(t.Elem()))
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		w.fields(t, props, &required, false)
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": []any{"string", "null"}, "contentEncoding": "base64"}
		}
		return map[string]any{"type": []any{"array", "null"}, "items": w.of(t.Elem())}
	case reflect.Array:
		return map[string]any{"type": "array", "items": w.of(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": []any{"object", "null"}, "additionalProperties": w.of(t.Elem())}
	case reflect.Interface:
		return map[string]any{} // any JSON value
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	}
	return map[string]any{} // not marshalled by our commands
}

// fields adds t's JSON fields to props. optional: t is reached through an
// embedded pointer, which may be nil (its fields then absent). A JSON name
// taken twice panics: encoding/json resolves such conflicts by depth and
// tags (or drops both), which the walker does not model.
func (w *walker) fields(t reflect.Type, props map[string]any, required *[]string, optional bool) {
	var inner []reflect.StructField
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		ft := f.Type
		if f.Anonymous && name == "" {
			et := ft
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				inner = append(inner, f)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if hasOpt(opts, "string") {
			panic("outschema: ,string tag on " + t.String() + "." + f.Name + " is not supported")
		}
		if _, dup := props[name]; dup {
			panic("outschema: JSON name " + name + " appears twice in " + t.String() + " (embedded field conflict)")
		}
		omit := hasOpt(opts, "omitempty") || hasOpt(opts, "omitzero")
		var s map[string]any
		switch {
		case omit && ft.Kind() == reflect.Pointer:
			// A nil pointer is omitted, so only its own null goes; what it
			// points to (a nil slice, say) can still marshal null.
			w.push(ft)
			s = w.of(ft.Elem())
			w.pop()
		case omit, f.Tag.Get("schema") == "nonnull":
			// An omitted nil slice or map never shows as null; nonnull:
			// the emitter guarantees a value.
			s = nonNull(w.of(ft))
		default:
			s = w.of(ft)
		}
		props[name] = s
		if !omit && !optional {
			*required = append(*required, name)
		}
	}
	for _, f := range inner {
		et, opt := f.Type, optional
		if et.Kind() == reflect.Pointer {
			et, opt = et.Elem(), true
		}
		w.push(et)
		w.fields(et, props, required, opt)
		w.pop()
	}
}

func hasOpt(opts, want string) bool {
	return slices.Contains(strings.Split(opts, ","), want)
}

// nullable adds null to s's type.
func nullable(s map[string]any) map[string]any {
	switch typ := s["type"].(type) {
	case string:
		s["type"] = []any{typ, "null"}
	case []any:
		if !slices.Contains(typ, any("null")) {
			s["type"] = append(typ, "null")
		}
	}
	return s
}

// nonNull removes null from s's type.
func nonNull(s map[string]any) map[string]any {
	if typ, ok := s["type"].([]any); ok {
		var keep []any
		for _, x := range typ {
			if x != "null" {
				keep = append(keep, x)
			}
		}
		if len(keep) == 1 {
			s["type"] = keep[0]
		} else {
			s["type"] = keep
		}
	}
	return s
}
