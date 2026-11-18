package bag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Obj is a JSON object for the canonical encoder. Key order is irrelevant;
// the encoder sorts keys as RFC 8785 requires.
type Obj map[string]any

// CanonicalJSON encodes v as RFC 8785 (JCS). It supports exactly the value
// types this format uses: Obj, []any, []Obj, string, int, int64, bool and nil.
// Floats are deliberately unsupported.
func CanonicalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeJCS(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encodeJCS(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		if x > 1<<53 || x < -(1<<53) {
			return fmt.Errorf("jcs: integer %d outside safe range", x)
		}
		b.WriteString(strconv.FormatInt(x, 10))
	case string:
		return encodeJCSString(b, x)
	case *string:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return encodeJCSString(b, *x)
	case []Obj:
		arr := make([]any, len(x))
		for i := range x {
			arr[i] = x[i]
		}
		return encodeJCS(b, arr)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeJCS(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case Obj:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeJCSString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := encodeJCS(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("jcs: unsupported type %T", v)
	}
	return nil
}

// encodeJCSString follows ECMAScript JSON.stringify: only '"', '\\' and
// control characters are escaped; everything else is literal UTF-8.
func encodeJCSString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("jcs: invalid UTF-8 in string")
	}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// decodeCanonical decodes data into v (unknown fields rejected) and then
// requires that re-encoding the value with enc yields exactly data.
func decodeCanonical(data []byte, v any, enc func() ([]byte, error)) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("kein gültiges UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return fmt.Errorf("trailing data")
	}
	again, err := enc()
	if err != nil {
		return err
	}
	if !bytes.Equal(again, data) {
		return fmt.Errorf("not in canonical form (RFC 8785)")
	}
	return nil
}
