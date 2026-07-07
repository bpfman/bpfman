package semantics

import (
	"reflect"
	"strings"

	bpfman "github.com/bpfman/bpfman"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Per-type OriginKind registry. The Shape registry maps each
// OriginKind to its static field schema; this one maps the
// concrete Go types those Values may be backed by to the same
// OriginKind. The two together let path access through a
// typed Value propagate both the underlying Go origin and the
// declared kind, so capability dispatch keeps working after
// sub-values are extracted ($prog = $loaded.programs[0]).
//
// The table is declarative: the bpfman domain types are part of
// the DSL's closed semantic world, so the shared substrate owns
// the mapping directly instead of asking cmd-side init to mutate
// it at startup.
var typeKind = map[reflect.Type]OriginKind{
	reflect.TypeFor[bpfman.Program]():       OriginProgram,
	reflect.TypeFor[bpfman.ProgramRecord](): OriginProgram,
	reflect.TypeFor[bpfman.Link]():          OriginLink,
	reflect.TypeFor[bpfman.LinkRecord]():    OriginLink,
}

// kindForType returns the OriginKind registered for t (or its
// pointer-to-t and elem-of-t alternatives so a *Program still
// resolves to OriginProgram), or OriginUnknown if no
// registration exists.
func kindForType(t reflect.Type) OriginKind {
	if t == nil {
		return OriginUnknown
	}
	if k, ok := typeKind[t]; ok {
		return k
	}
	if t.Kind() == reflect.Pointer {
		if k, ok := typeKind[t.Elem()]; ok {
			return k
		}
	}
	return OriginUnknown
}

// walkOrigin walks the Go value rooted at origin through the
// same path steps walkPath uses on the JSON-tree mirror,
// returning the sub-value at the end of the path or nil if any
// step fails (no field, index out of range, nil pointer,
// incompatible kind). The caller passes steps already parsed
// from parsePath; this mirrors walkPath's traversal so the two
// stay in lockstep.
//
// Struct fields are resolved by JSON tag (or the Go field name
// when no tag is present), matching encoding/json's marshal
// rules. Pointers and interfaces are unwrapped transparently.
// Maps and unrecognised kinds yield nil -- the caller (LookupValue)
// treats nil as "origin lost beyond this point" and continues
// with the JSON-tree walk only.
func walkOrigin(origin any, steps []syntax.PathStep) any {
	if origin == nil {
		return nil
	}
	v := reflect.ValueOf(origin)
	for _, step := range steps {
		v = unwrap(v)
		if !v.IsValid() {
			return nil
		}
		if step.IsIndex {
			if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
				return nil
			}
			if step.Index < 0 || step.Index >= v.Len() {
				return nil
			}
			v = v.Index(step.Index)
			continue
		}
		if v.Kind() != reflect.Struct {
			return nil
		}
		f, ok := jsonFieldByName(v.Type(), step.Field)
		if !ok {
			return nil
		}
		v = v.FieldByIndex(f.Index)
	}
	v = unwrap(v)
	if !v.IsValid() {
		return nil
	}
	if !v.CanInterface() {
		return nil
	}
	return v.Interface()
}

// WalkSuborigin follows steps through origin and returns both the
// resulting sub-origin and its registered OriginKind. A missing or
// unregistered sub-origin yields (nil, OriginUnknown).
func WalkSuborigin(origin any, steps []syntax.PathStep) (any, OriginKind) {
	sub := walkOrigin(origin, steps)
	if sub == nil {
		return nil, OriginUnknown
	}
	return sub, kindForType(reflect.TypeOf(sub))
}

// unwrap dereferences pointers and unwraps interfaces until v
// is a concrete value (or invalid). Nil pointers / interfaces
// resolve to an invalid Value, which walkOrigin treats as a
// terminal failure.
func unwrap(v reflect.Value) reflect.Value {
	for {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		default:
			return v
		}
	}
}

// jsonFieldByName finds the struct field of t whose JSON-tag
// name is name. Falls back to the Go field name when the tag
// is absent. Anonymous fields without a json tag are inlined
// (per encoding/json's flattening rule) and their fields are
// searched recursively.
func jsonFieldByName(t reflect.Type, name string) (reflect.StructField, bool) {
	if t.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		jsonName, omit := parseJSONTag(tag)
		if omit {
			continue
		}
		// Anonymous fields with no json tag inline their
		// inner fields into the parent's JSON shape; recurse
		// to keep walkOrigin in lockstep with the JSON walk.
		if f.Anonymous && tag == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if inner, ok := jsonFieldByName(ft, name); ok {
					inner.Index = append([]int{i}, inner.Index...)
					return inner, true
				}
			}
			continue
		}
		if jsonName == "" {
			jsonName = f.Name
		}
		if jsonName == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// parseJSONTag splits a json struct tag, returning the field
// name component and whether the field is omitted entirely
// (tag == "-"). The options after the comma (omitempty etc.)
// are not relevant for name lookup.
func parseJSONTag(tag string) (name string, omit bool) {
	if tag == "-" {
		return "", true
	}
	if before, _, ok := strings.Cut(tag, ","); ok {
		return before, false
	}
	return tag, false
}
