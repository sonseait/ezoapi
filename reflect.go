package ezoapi

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Reflector can override wire representations, e.g. decimal.Decimal or UUID.
// Configure TypeSchemas before registering routes; do not mutate it concurrently.
type Reflector struct{ TypeSchemas map[reflect.Type]*Schema }

// Schema derives a schema from a DTO or reflect.Type. Typed nil pointers are safe.
// No sample data or field-name heuristics are used to guess a wire representation.
func (r Reflector) Schema(dto any) (*Schema, error) {
	if s, ok := dto.(*Schema); ok {
		return prepareSchema(s)
	}
	if s, ok := dto.(Schema); ok {
		return prepareSchema(&s)
	}
	t, ok := dto.(reflect.Type)
	if !ok {
		t = reflect.TypeOf(dto)
	}
	if t == nil {
		return nil, fmt.Errorf("ezoapi: nil DTO has no type")
	}
	b := schemaBuilder{reflector: r, defs: map[string]*Schema{}}
	s, err := b.build(t)
	if err != nil {
		return nil, err
	}
	if len(b.defs) > 0 {
		s.Defs = b.defs
	}
	if err := validateSchema(s); err != nil {
		return nil, err
	}
	if err := identify(s); err != nil {
		return nil, err
	}
	return s, nil
}

// Each schema is its own resource so local $defs references stay valid when
// embedded in an OpenAPI document instead of resolving against its root.
func identify(s *Schema) error {
	if s.Boolean != nil || s.ID != "" {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	s.ID = fmt.Sprintf("urn:ezoapi:schema:%x", hash)
	return nil
}

type schemaBuilder struct {
	reflector Reflector
	defs      map[string]*Schema
}

func nullable(s *Schema) *Schema {
	return &Schema{AnyOf: []*Schema{s, {Type: "null"}}}
}

func (b *schemaBuilder) build(t reflect.Type) (*Schema, error) {
	if s := b.reflector.TypeSchemas[t]; s != nil {
		return b.override(s)
	}
	if t.Kind() == reflect.Pointer {
		if indirectType(t) == nil {
			return &Schema{Type: "null"}, nil
		}
		s, err := b.build(t.Elem())
		if err != nil {
			return nil, err
		}
		return nullable(s), nil
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return &Schema{}, nil
	}
	if t == reflect.TypeFor[json.Number]() {
		return &Schema{Type: "number"}, nil
	}
	if t == reflect.TypeFor[time.Time]() {
		return &Schema{Type: "string", Format: "date-time"}, nil
	}
	if t.Implements(reflect.TypeFor[json.Marshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Marshaler]()) {
		return nil, fmt.Errorf("ezoapi: %v implements json.Marshaler; provide Reflector.TypeSchemas", t)
	}
	if t.Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
		return &Schema{Type: "string"}, nil
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
		return nil, fmt.Errorf("ezoapi: %v has an addressability-dependent TextMarshaler; provide Reflector.TypeSchemas", t)
	}
	if t.Name() != "" && (t.Kind() == reflect.Struct || t.Kind() == reflect.Slice || t.Kind() == reflect.Map || t.Kind() == reflect.Array) {
		hash := sha256.Sum256([]byte(t.PkgPath() + ":" + t.String()))
		key := fmt.Sprintf("T%x", hash[:12])
		ref := &Schema{Ref: "#/$defs/" + key}
		if b.defs[key] != nil {
			return ref, nil
		}
		placeholder := &Schema{}
		b.defs[key] = placeholder
		s, err := b.kind(t)
		if err != nil {
			return nil, err
		}
		*placeholder = *s
		return ref, nil
	}
	return b.kind(t)
}

func (b *schemaBuilder) override(s *Schema) (*Schema, error) {
	clone, err := prepareSchema(s)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(clone)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	key := fmt.Sprintf("S%x", hash[:12])
	b.defs[key] = clone
	return &Schema{Ref: "#/$defs/" + key}, nil
}

func (b *schemaBuilder) kind(t reflect.Type) (*Schema, error) {
	s := &Schema{}
	switch t.Kind() {
	case reflect.String:
		s.Type = "string"
	case reflect.Bool:
		s.Type = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s.Type = "integer"
		if t.Bits() <= 32 {
			s.Format = "int32"
		} else {
			s.Format = "int64"
		}
		min := int64(-1) << (t.Bits() - 1)
		s.Minimum, s.Maximum = min, ^min
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s.Type = "integer"
		s.Minimum, s.Maximum = 0, ^uint64(0)>>(64-t.Bits())
	case reflect.Float32, reflect.Float64:
		s.Type = "number"
		s.Format = "double"
		if t.Kind() == reflect.Float32 {
			s.Format = "float"
		}
	case reflect.Interface:
		// An interface can contain any JSON value, including scalars and null.
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 && !reflect.PointerTo(t.Elem()).Implements(reflect.TypeFor[json.Marshaler]()) && !reflect.PointerTo(t.Elem()).Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
			return nullable(&Schema{Type: "string", ContentEncoding: "base64"}), nil
		}
		s.Type = "array"
		item, err := b.build(t.Elem())
		if err != nil {
			return nil, err
		}
		s.Items = item
		if t.Kind() == reflect.Array {
			s.MinItems = ptr(t.Len())
			s.MaxItems = ptr(t.Len())
		} else {
			s = nullable(s)
		}
	case reflect.Map:
		k := t.Key()
		if k.Kind() != reflect.String && !(k.Kind() >= reflect.Int && k.Kind() <= reflect.Uintptr) && !k.Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
			return nil, fmt.Errorf("ezoapi: unsupported JSON map key %v", k)
		}
		s.Type = "object"
		value, err := b.build(t.Elem())
		if err != nil {
			return nil, err
		}
		s.AdditionalProperties = value
		s = nullable(s)
	case reflect.Struct:
		if err := b.object(t, s); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("ezoapi: unsupported JSON type %v", t)
	}
	return s, nil
}

func ptr[T any](v T) *T { return &v }

func indirectType(t reflect.Type) reflect.Type {
	seen := map[reflect.Type]bool{}
	for t != nil && t.Kind() == reflect.Pointer {
		if seen[t] {
			return nil
		}
		seen[t] = true
		t = t.Elem()
	}
	return t
}

type jsonField struct {
	field                    reflect.StructField
	name                     string
	depth                    int
	tagged, optional, quoted bool
}

// jsonFields applies encoding/json's depth and tagged-field dominance rules.
// reflect.VisibleFields alone cannot model fields with different JSON names.
func jsonFields(t reflect.Type, nameTag string) []jsonField {
	candidates := map[string][]jsonField{}
	var walk func(reflect.Type, int, bool, map[reflect.Type]bool)
	walk = func(t reflect.Type, depth int, optional bool, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.PkgPath != "" && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			tag, found := f.Tag.Lookup(nameTag)
			if !found && nameTag != "json" {
				tag = f.Tag.Get("json")
			}
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "-" {
				continue
			}
			if !validName(name) {
				name = ""
			}
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft, depth+1, optional || f.Type.Kind() == reflect.Pointer, seen)
				continue
			}
			if f.PkgPath != "" {
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			omitEmpty := slices.Contains(parts[1:], "omitempty") && f.Type.Kind() != reflect.Struct && (f.Type.Kind() != reflect.Array || f.Type.Len() == 0)
			candidates[name] = append(candidates[name], jsonField{f, name, depth, tagged, optional || omitEmpty || slices.Contains(parts[1:], "omitzero"), slices.Contains(parts[1:], "string")})
		}
	}
	walk(t, 0, false, map[reflect.Type]bool{})
	var fields []jsonField
	for _, group := range candidates {
		slices.SortStableFunc(group, func(a, b jsonField) int {
			if a.depth != b.depth {
				return a.depth - b.depth
			}
			if a.tagged == b.tagged {
				return 0
			}
			if a.tagged {
				return -1
			}
			return 1
		})
		if len(group) > 1 && group[0].depth == group[1].depth && group[0].tagged == group[1].tagged {
			continue
		}
		fields = append(fields, group[0])
	}
	slices.SortFunc(fields, func(a, b jsonField) int { return strings.Compare(a.name, b.name) })
	return fields
}

func validName(s string) bool {
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c) {
			return false
		}
	}
	return s != ""
}

func (b *schemaBuilder) object(t reflect.Type, s *Schema) error {
	s.Type = "object"
	s.Properties = map[string]*Schema{}
	for _, f := range jsonFields(t, "json") {
		p, err := b.field(f)
		if err != nil {
			return fmt.Errorf("%v.%s: %w", t, f.field.Name, err)
		}
		s.Properties[f.name] = p
		if !f.optional || slices.Contains(strings.Split(f.field.Tag.Get("validate"), ","), "required") || f.field.Tag.Get("required") == "true" {
			s.Required = append(s.Required, f.name)
		}
	}
	return nil
}

func (b *schemaBuilder) field(f jsonField) (*Schema, error) {
	var s *Schema
	var err error
	raw, overridden := f.field.Tag.Lookup("schema")
	if overridden {
		s = &Schema{}
		err = json.Unmarshal([]byte(raw), s)
		if err == nil {
			s, err = b.override(s)
		}
	} else {
		s, err = b.build(f.field.Type)
	}
	if err != nil {
		return nil, err
	}
	if s.Boolean != nil {
		s = &Schema{AllOf: []*Schema{s}}
	}
	t := indirectType(f.field.Type)
	if t == nil {
		t = reflect.TypeFor[any]()
	}
	custom := t.Implements(reflect.TypeFor[json.Marshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Marshaler]()) || t.Implements(reflect.TypeFor[encoding.TextMarshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextMarshaler]())
	if f.quoted && !custom && !overridden && (t.Kind() == reflect.String || t.Kind() == reflect.Bool || (t.Kind() >= reflect.Int && t.Kind() <= reflect.Float64)) {
		inner, err := b.build(t)
		if err != nil {
			return nil, err
		}
		if err := constraints(inner, t, f.field.Tag.Get("validate")); err != nil {
			return nil, err
		}
		s = &Schema{Type: "string", ContentMediaType: "application/json", ContentSchema: inner}
		if f.field.Type.Kind() == reflect.Pointer {
			s = nullable(s)
		}
	}
	if description, ok := f.field.Tag.Lookup("description"); ok {
		s.Description = description
	}
	if !f.quoted && !(t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8) {
		if err := constraints(s, t, f.field.Tag.Get("validate")); err != nil {
			return nil, err
		}
	}
	if format := f.field.Tag.Get("format"); format != "" {
		s.Format = format
	}
	stringWire := schemaWireType(s, b.defs, 0) == "string"
	for _, tag := range []string{"example", "default", "enum"} {
		if raw, ok := f.field.Tag.Lookup(tag); ok {
			v, err := decodeJSON([]byte(raw))
			if err != nil {
				if tag != "enum" && stringWire {
					v = raw
				} else {
					return nil, fmt.Errorf("invalid %s JSON: %w", tag, err)
				}
			}
			if tag != "enum" && stringWire {
				if _, isString := v.(string); !isString && v != nil {
					v = raw
				}
			}
			if tag != "enum" && !overridden && b.reflector.TypeSchemas[f.field.Type] == nil && b.reflector.TypeSchemas[t] == nil {
				if err := fieldValue(f, v); err != nil {
					return nil, fmt.Errorf("invalid %s for %v: %w", tag, f.field.Type, err)
				}
			}
			switch tag {
			case "example":
				s.Examples = []any{v}
			case "default":
				data, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				s.Default = data
			case "enum":
				values, ok := v.([]any)
				if !ok || len(values) == 0 {
					return nil, fmt.Errorf("enum must be a non-empty JSON array")
				}
				s.Enum = values
			}
		}
	}
	return s, nil
}

func fieldValue(f jsonField, v any) error {
	t := indirectType(f.field.Type)
	if t == nil {
		return nil
	}
	if t != reflect.TypeFor[time.Time]() && (t.Implements(reflect.TypeFor[json.Marshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Marshaler]()) || t.Implements(reflect.TypeFor[encoding.TextMarshaler]()) || reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextMarshaler]())) {
		return nil
	}
	tag := `json:"value"`
	if f.quoted {
		tag = `json:"value,string"`
	}
	wrapper := reflect.StructOf([]reflect.StructField{{Name: "Value", Type: f.field.Type, Tag: reflect.StructTag(tag)}})
	data, err := json.Marshal(map[string]any{"value": v})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, reflect.New(wrapper).Interface())
}

// Only unconditional validator rules with equivalent JSON Schema semantics are
// translated. Conditional, custom and dive rules stay with the application's validator.
func constraints(s *Schema, t reflect.Type, tag string) error {
	for _, rule := range strings.Split(tag, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "dive" {
			break
		}
		switch rule {
		case "email", "uuid", "ipv4", "ipv6", "hostname":
			if t.Kind() == reflect.String {
				s.Format = rule
			}
		case "url", "uri":
			if t.Kind() == reflect.String {
				s.Format = "uri"
			}
		case "uuid4":
			if t.Kind() == reflect.String {
				s.Format = "uuid"
				s.Pattern = `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
			}
		case "unique":
			if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
				s.UniqueItems = true
			}
		}
		name, value, ok := strings.Cut(rule, "=")
		if !ok {
			continue
		}
		if name == "oneof" {
			values, err := validatorEnum(t, value)
			if err != nil {
				return err
			}
			s.Enum = values
			continue
		}
		if !slices.Contains([]string{"min", "max", "len", "gte", "lte", "gt", "lt"}, name) {
			continue
		}
		if t.Kind() == reflect.String || t.Kind() == reflect.Array || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			if name == "gte" || name == "lte" || name == "gt" || name == "lt" {
				continue
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return fmt.Errorf("invalid validation rule %q", rule)
			}
			var min, max **int
			switch t.Kind() {
			case reflect.String:
				min, max = &s.MinLength, &s.MaxLength
			case reflect.Map:
				min, max = &s.MinProperties, &s.MaxProperties
			default:
				min, max = &s.MinItems, &s.MaxItems
			}
			if name == "min" || name == "len" {
				*min = ptr(n)
			}
			if name == "max" || name == "len" {
				*max = ptr(n)
			}
		} else if t.Kind() >= reflect.Int && t.Kind() <= reflect.Float64 {
			n := json.Number(value)
			data, err := json.Marshal(n)
			if err != nil {
				return fmt.Errorf("invalid validation rule %q", rule)
			}
			bound, ok := new(big.Rat).SetString(string(data))
			if !ok {
				return fmt.Errorf("invalid validation rule %q", rule)
			}
			if name == "gt" && (s.ExclusiveMinimum == nil || compareBound(bound, s.ExclusiveMinimum) > 0) {
				s.ExclusiveMinimum = n
			}
			if name == "lt" && (s.ExclusiveMaximum == nil || compareBound(bound, s.ExclusiveMaximum) < 0) {
				s.ExclusiveMaximum = n
			}
			if name == "min" || name == "gte" || name == "len" {
				if s.Minimum == nil || compareBound(bound, s.Minimum) > 0 {
					s.Minimum = n
				}
			}
			if name == "max" || name == "lte" || name == "len" {
				if s.Maximum == nil || compareBound(bound, s.Maximum) < 0 {
					s.Maximum = n
				}
			}
		}
	}
	return nil
}

var oneOfTokens = regexp.MustCompile(`'[^']*'|\S+`)

func validatorEnum(t reflect.Type, raw string) ([]any, error) {
	tokens := oneOfTokens.FindAllString(raw, -1)
	if len(tokens) == 0 || strings.Count(raw, "'")%2 != 0 {
		return nil, fmt.Errorf("ezoapi: invalid oneof rule %q", raw)
	}
	values := make([]any, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ReplaceAll(token, "'", "")
		switch t.Kind() {
		case reflect.String:
			values = append(values, token)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			value, err := strconv.ParseInt(token, 10, t.Bits())
			if err != nil {
				return nil, fmt.Errorf("ezoapi: invalid %v oneof value %q", t, token)
			}
			values = append(values, json.Number(strconv.FormatInt(value, 10)))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			value, err := strconv.ParseUint(token, 10, t.Bits())
			if err != nil {
				return nil, fmt.Errorf("ezoapi: invalid %v oneof value %q", t, token)
			}
			values = append(values, json.Number(strconv.FormatUint(value, 10)))
		default:
			return nil, fmt.Errorf("ezoapi: oneof needs a string/integer type, got %v; use enum for JSON values", t)
		}
	}
	return values, nil
}

func compareBound(n *big.Rat, bound any) int {
	r, _ := new(big.Rat).SetString(fmt.Sprint(bound))
	if r == nil {
		return 0
	} // compileSchema reports malformed override bounds.
	return n.Cmp(r)
}

func decodeJSON(data []byte) (any, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}

// ValidateExample checks a JSON-serializable value without converting numbers
// through float64. The returned RawMessage preserves its exact wire example.
func ValidateExample(s *Schema, value any) (json.RawMessage, error) {
	if s == nil {
		return nil, fmt.Errorf("ezoapi: nil example schema")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("ezoapi: marshal example: %w", err)
	}
	v, err := decodeJSON(data)
	if err != nil {
		return nil, err
	}
	resolved, err := compileSchema(s)
	if err != nil {
		return nil, err
	}
	if err := resolved.Validate(v); err != nil {
		return nil, fmt.Errorf("ezoapi: example does not match schema: %w", err)
	}
	return data, nil
}

func compileSchema(s *Schema) (*jsonschema.Schema, error) {
	c, _, err := schemaCompiler(s)
	if err != nil {
		return nil, err
	}
	return c.Compile(schemaURI)
}
