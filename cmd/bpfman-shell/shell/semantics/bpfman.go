package semantics

import (
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"time"

	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// Program and Link shape inference for bpfman API types. Derived
// from the bpfman package via reflect so the registry tracks the
// real Go types without hand-maintained mirror declarations.

var (
	typeToOriginKind = map[reflect.Type]OriginKind{
		reflect.TypeFor[bpfman.Program](): OriginProgram,
		reflect.TypeFor[bpfman.Link]():    OriginLink,
	}

	programShape = shapeFromType(reflect.TypeFor[bpfman.Program](), OriginProgram)
	linkShape    = shapeFromType(reflect.TypeFor[bpfman.Link](), OriginLink)

	loadResultShape     = shapeFromType(reflect.TypeFor[bpfman.LoadResult](), OriginUnknown)
	programListShape    = shapeFromType(reflect.TypeFor[bpfman.ProgramListResult](), OriginUnknown)
	linkListResultShape = shapeFromType(reflect.TypeFor[bpfman.LinkListResult](), OriginUnknown)

	linkDetailsShapes = buildLinkDetailsShapes()
)

func buildLinkDetailsShapes() map[string]Shape {
	shapes := map[string]Shape{}
	for _, kind := range bpfman.LinkAttachKinds() {
		t := bpfman.LinkAttachKindDetailsType(kind)
		if t == nil {
			continue
		}
		shapes[kind] = shapeFromType(t, OriginUnknown)
	}
	return shapes
}

func inferBpfmanBindShape(args []syntax.Expr) Shape {
	if len(args) < 2 {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	noun, ok := args[0].(*syntax.LiteralExpr)
	if !ok || noun.Quoted {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	verb, ok := args[1].(*syntax.LiteralExpr)
	if !ok || verb.Quoted {
		return Shape{Sealed: false, Kind: OriginUnknown}
	}
	switch noun.Text {
	case "program":
		switch verb.Text {
		case "load":
			return loadResultShape
		case "get":
			return KindShape(OriginProgram)
		case "list":
			return programListShape
		}
	case "link":
		switch verb.Text {
		case "attach":
			if len(args) >= 3 {
				if kind, ok := args[2].(*syntax.LiteralExpr); ok && !kind.Quoted {
					if details, ok := linkDetailsShapes[kind.Text]; ok {
						return linkShapeWithDetails(details)
					}
				}
			}
			return KindShape(OriginLink)
		case "get":
			return KindShape(OriginLink)
		case "list":
			return linkListResultShape
		}
	}
	return Shape{Sealed: false, Kind: OriginUnknown}
}

func linkShapeWithDetails(detailsShape Shape) Shape {
	link := CloneShape(KindShape(OriginLink))
	record, ok := link.Fields["record"]
	if !ok {
		return link
	}
	if _, ok := record.Fields["details"]; !ok {
		return link
	}
	record.Fields["details"] = detailsShape
	link.Fields["record"] = record
	return link
}

func shapeFromType(t reflect.Type, kind OriginKind) Shape {
	return buildShape(t, map[reflect.Type]bool{}, kind)
}

// ShapeFromType derives the inferred Shape for a Go type -- the public
// entry to the same derivation the bind-shape registry uses. Tests use
// it to assert that a decoded value's type matches a command's static
// shape.
func ShapeFromType(t reflect.Type) Shape {
	return shapeFromType(t, OriginUnknown)
}

func buildShape(t reflect.Type, seen map[reflect.Type]bool, kind OriginKind) Shape {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if mapped, ok := typeToOriginKind[t]; ok && kind == OriginUnknown {
		kind = mapped
	}
	if t == reflect.TypeFor[time.Time]() {
		return Shape{Sealed: true, Kind: OriginScalar}
	}
	if t == reflect.TypeFor[json.Number]() {
		return Shape{Sealed: true, Kind: OriginScalar}
	}
	if implementsJSONMarshaler(t) {
		return Shape{Sealed: false, Kind: kind}
	}
	if seen[t] {
		return Shape{Sealed: false, Kind: kind}
	}
	switch t.Kind() {
	case reflect.Struct:
		seen[t] = true
		defer delete(seen, t)
		fields := map[string]Shape{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, omit := jsonFieldName(f)
			if omit {
				continue
			}
			child := buildShape(f.Type, seen, OriginUnknown)
			if f.Anonymous && f.Tag.Get("json") == "" {
				if child.Sealed {
					maps.Copy(fields, child.Fields)
				}
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[name] = child
		}
		return Shape{Sealed: true, Kind: kind, Fields: fields}
	case reflect.Slice, reflect.Array:
		elem := buildShape(t.Elem(), seen, OriginUnknown)
		return Shape{Sealed: false, Kind: kind, Elem: &elem}
	case reflect.Map, reflect.Interface:
		return Shape{Sealed: false, Kind: OriginMap}
	case reflect.Bool:
		return Shape{Sealed: true, Kind: OriginBool}
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return Shape{Sealed: true, Kind: OriginScalar}
	}
	return Shape{Sealed: false, Kind: kind}
}

func jsonFieldName(f reflect.StructField) (name string, omit bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if tag == "" {
		return f.Name, false
	}
	if comma := strings.Index(tag, ","); comma >= 0 {
		tag = tag[:comma]
	}
	return tag, false
}

var jsonMarshalerType = reflect.TypeFor[json.Marshaler]()

func implementsJSONMarshaler(t reflect.Type) bool {
	if t.Implements(jsonMarshalerType) {
		return true
	}
	return reflect.PointerTo(t).Implements(jsonMarshalerType)
}
