// AST dumping for diagnostics. The reflective dumper walks a
// parsed Program and emits a Go-shaped tree with one field per
// line, matching the rough form of go/ast.Print so users
// already familiar with that idiom can read the output without
// new vocabulary. Used by 'bpfman-shell --ast FILE' to expose
// what the parser actually built
// from a piece of source -- handy for understanding precedence,
// for verifying that an expression bound the way you expected,
// and as a teaching aid for the surface syntax.
//
// Reflective so that adding a new Stmt or Expr variant does not
// require touching the dumper. The cost is that field tags or
// special-case formatting (e.g. hiding empty source.Pos values) live
// in the dumper's render logic rather than on each AST type.

package syntax

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// DumpAST writes a textual tree representation of node to w.
// node may be a *Program, any Stmt, or any Expr. Returns the
// first write error encountered. Indentation is two spaces per
// level so trees stay scannable in narrow terminals.
func DumpAST(w io.Writer, node any) error {
	var b strings.Builder
	dumpValue(&b, reflect.ValueOf(node), 0, "")
	_, err := io.WriteString(w, b.String())
	return err
}

// dumpValue is the reflective walker. It dispatches on Kind:
// pointers and interfaces are unwrapped; structs print their
// type name and recurse over fields; slices print their length
// and recurse over elements; everything else is rendered with
// %v in the most compact form that disambiguates the value.
//
// label is the field/index label printed before the value, or
// the empty string at the root.
func dumpValue(b *strings.Builder, v reflect.Value, depth int, label string) {
	indent := strings.Repeat("  ", depth)

	// Unwrap interfaces and pointers transparently so the user
	// sees the underlying concrete type.
	for {
		if !v.IsValid() {
			fmt.Fprintf(b, "%s%s<nil>\n", indent, formatLabel(label))
			return
		}
		k := v.Kind()
		if k == reflect.Interface || k == reflect.Pointer {
			if v.IsNil() {
				fmt.Fprintf(b, "%s%snil\n", indent, formatLabel(label))
				return
			}
			v = v.Elem()
			continue
		}
		break
	}

	switch v.Kind() {
	case reflect.Struct:
		dumpStruct(b, v, depth, label)
	case reflect.Slice, reflect.Array:
		dumpSlice(b, v, depth, label)
	default:
		fmt.Fprintf(b, "%s%s%s\n", indent, formatLabel(label), formatScalar(v))
	}
}

// dumpStruct prints a struct as 'TypeName {' followed by one
// line per non-zero field and a closing '}'. Embedded source.Pos and
// source.Span fields render as a single shorthand line because every
// AST node carries a source.Span and the full nested form would more
// than double the dump's vertical space.
func dumpStruct(b *strings.Builder, v reflect.Value, depth int, label string) {
	indent := strings.Repeat("  ", depth)
	t := v.Type()
	fmt.Fprintf(b, "%s%s%s {\n", indent, formatLabel(label), t.Name())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		fv := v.Field(i)
		if isZero(fv) {
			continue
		}
		// source.Span shorthand: render as "Span: line:col-line:col".
		if sp, ok := fv.Interface().(source.Span); ok {
			fmt.Fprintf(b, "%s  Span: %d:%d-%d:%d\n", indent,
				sp.Pos.Line, sp.Pos.Col, sp.End.Line, sp.End.Col)
			continue
		}
		// source.Pos shorthand for any non-embedded source.Pos field.
		if loc, ok := fv.Interface().(source.Pos); ok {
			fmt.Fprintf(b, "%s  %s: %d:%d\n", indent, field.Name, loc.Line, loc.Col)
			continue
		}
		dumpValue(b, fv, depth+1, field.Name)
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// dumpSlice prints a slice as '[]T (len = N) {' followed by
// one entry per element labelled by index, and a closing '}'.
func dumpSlice(b *strings.Builder, v reflect.Value, depth int, label string) {
	indent := strings.Repeat("  ", depth)
	if v.Len() == 0 {
		fmt.Fprintf(b, "%s%s[] (empty)\n", indent, formatLabel(label))
		return
	}
	elemType := v.Type().Elem().String()
	fmt.Fprintf(b, "%s%s[]%s (len = %d) {\n", indent, formatLabel(label), elemType, v.Len())
	for i := 0; i < v.Len(); i++ {
		dumpValue(b, v.Index(i), depth+1, fmt.Sprintf("[%d]", i))
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// formatLabel renders a field or index label with a trailing
// ': '; an empty label produces no prefix, used at the root
// of the dump.
func formatLabel(label string) string {
	if label == "" {
		return ""
	}
	return label + ": "
}

// formatScalar renders a leaf value compactly. Strings are
// quoted with %q so empty/whitespace values are visible;
// booleans and numbers use the default %v.
func formatScalar(v reflect.Value) string {
	if v.Kind() == reflect.String {
		return fmt.Sprintf("%q", v.String())
	}
	return fmt.Sprintf("%v", v.Interface())
}

// isZero reports whether v is the zero value of its type. Used
// to skip noise fields (empty strings, zero ints, false flags,
// nil pointers, empty slices) so the dump shows only what the
// parser actually populated. False positives are mostly
// harmless: a deliberately-zero field is omitted but the user
// reading the dump can infer it from context.
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return v.IsNil()
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isZero(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		// source.Pos{} and source.Span{} are the only structs we want to
		// elide; everything else is treated as non-zero so the
		// dump preserves shape even for default-looking values.
		if loc, ok := v.Interface().(source.Pos); ok {
			return loc.Line == 0 && loc.Col == 0
		}
		if sp, ok := v.Interface().(source.Span); ok {
			return sp.Pos.Line == 0 && sp.Pos.Col == 0 &&
				sp.End.Line == 0 && sp.End.Col == 0
		}
		return false
	}
	return false
}
