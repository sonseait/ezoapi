package ezoapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// BoolSchema returns a JSON Schema that accepts every value (true) or none (false).
func BoolSchema(value bool) *Schema { return &Schema{Boolean: &value} }

type schemaJSON Schema

func schemaKeyword(name string) bool {
	t := reflect.TypeFor[schemaJSON]()
	for i := 0; i < t.NumField(); i++ {
		if strings.Split(t.Field(i).Tag.Get("json"), ",")[0] == name {
			return name != "-"
		}
	}
	return false
}

// MarshalJSON preserves boolean schemas, type unions and unknown keywords.
// Type and Types are mutually exclusive; Extra cannot replace typed fields.
func (s Schema) MarshalJSON() ([]byte, error) {
	return marshalSchema(s, map[*Schema]bool{})
}

func marshalSchema(s Schema, seen map[*Schema]bool) ([]byte, error) {
	if s.Type != "" && s.Types != nil {
		return nil, fmt.Errorf("ezoapi: use Type or Types, not both")
	}
	object := map[string]json.RawMessage{}
	value := reflect.ValueOf(s)
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		v := value.Field(i)
		keepEmpty := name == "enum" || name == "anyOf" || name == "oneOf" || name == "allOf" || name == "prefixItems"
		if v.IsZero() || ((v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.Len() == 0 && !keepEmpty) {
			continue
		}
		data, err := marshalSchemaValue(v.Interface(), seen)
		if err != nil {
			return nil, err
		}
		object[name] = data
	}
	if s.Boolean != nil {
		if len(object) != 0 || s.Types != nil || len(s.Extra) != 0 {
			return nil, fmt.Errorf("ezoapi: boolean schema cannot have sibling keywords")
		}
		return json.Marshal(*s.Boolean)
	}
	if s.Types != nil {
		data, err := json.Marshal(s.Types)
		if err != nil {
			return nil, err
		}
		object["type"] = data
	}
	for key, value := range s.Extra {
		if schemaKeyword(key) {
			return nil, fmt.Errorf("ezoapi: Extra cannot override schema keyword %q", key)
		}
		data, err := marshalSchemaValue(value, seen)
		if err != nil {
			return nil, err
		}
		object[key] = data
	}
	return json.Marshal(object)
}

func marshalSchemaValue(value any, seen map[*Schema]bool) ([]byte, error) {
	switch v := value.(type) {
	case *Schema:
		if v == nil {
			return []byte("null"), nil
		}
		if seen[v] {
			return nil, fmt.Errorf("ezoapi: cyclic schema pointers; use $ref for recursion")
		}
		seen[v] = true
		defer delete(seen, v)
		return marshalSchema(*v, seen)
	case Schema:
		return marshalSchema(v, seen)
	case map[string]*Schema:
		fields := map[string]json.RawMessage{}
		for key, child := range v {
			data, err := marshalSchemaValue(child, seen)
			if err != nil {
				return nil, err
			}
			fields[key] = data
		}
		return json.Marshal(fields)
	case []*Schema:
		items := make([]json.RawMessage, len(v))
		for i, child := range v {
			data, err := marshalSchemaValue(child, seen)
			if err != nil {
				return nil, err
			}
			items[i] = data
		}
		return json.Marshal(items)
	default:
		return json.Marshal(value)
	}
}

func (s *Schema) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("true")) || bytes.Equal(data, []byte("false")) {
		*s = *BoolSchema(bytes.Equal(data, []byte("true")))
		return nil
	}
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("ezoapi: schema must be an object or boolean")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var result schemaJSON
	// Decode type separately because JSON Schema permits a string or string array.
	typed := object["type"]
	delete(object, "type")
	encoded, err := json.Marshal(object)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	if err := d.Decode(&result); err != nil {
		return err
	}
	if typed != nil {
		if len(typed) > 0 && typed[0] == '[' {
			err = json.Unmarshal(typed, &result.Types)
		} else {
			err = json.Unmarshal(typed, &result.Type)
		}
		if err != nil {
			return fmt.Errorf("ezoapi: invalid schema type: %w", err)
		}
		if bytes.Equal(typed, []byte("null")) {
			return fmt.Errorf("ezoapi: schema type cannot be JSON null")
		}
	}
	for key, value := range object {
		if schemaKeyword(key) {
			continue
		}
		if result.Extra == nil {
			result.Extra = map[string]any{}
		}
		v, err := decodeJSON(value)
		if err != nil {
			return err
		}
		result.Extra[key] = v
	}
	*s = Schema(result)
	return nil
}

func cloneSchema(s *Schema) (*Schema, error) {
	if s == nil {
		return nil, fmt.Errorf("ezoapi: nil schema")
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var clone Schema
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func prepareSchema(s *Schema) (*Schema, error) {
	clone, err := cloneSchema(s)
	if err != nil {
		return nil, err
	}
	if err := validateSchema(clone); err != nil {
		return nil, err
	}
	if err := identify(clone); err != nil {
		return nil, err
	}
	return clone, nil
}

const schemaURI = "https://ezoapi.local/schema.json"

func schemaCompiler(s *Schema) (*jsonschema.Compiler, any, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, nil, err
	}
	doc, err := decodeJSON(data)
	if err != nil {
		return nil, nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	c.AssertContent()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	if err := c.AddResource(schemaURI, doc); err != nil {
		return nil, nil, err
	}
	return c, doc, nil
}

// Compile annotations at their original JSON Pointer locations, preserving
// reference scopes in nested schemas and checking even unused $defs.
func validateSchema(s *Schema) error {
	c, doc, err := schemaCompiler(s)
	if err != nil {
		return err
	}
	base, _ := url.Parse(schemaURI)
	var walk func(any, string, *url.URL) error
	walk = func(value any, path string, base *url.URL) error {
		location := schemaURI + (&url.URL{Fragment: path}).String()
		compiled, err := c.Compile(location)
		if err != nil {
			return err
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		if id, ok := object["$id"].(string); ok {
			ref, err := url.Parse(id)
			if err != nil {
				return err
			}
			base = base.ResolveReference(ref)
			// Compile accepts retrieval URLs; register embedded resource IDs as
			// retrieval aliases for discriminator mappings to those resources.
			if err := c.AddResource(base.String(), value); err != nil {
				return err
			}
		}
		for _, keyword := range []string{"examples", "enum"} {
			if values, ok := object[keyword].([]any); ok {
				for _, sample := range values {
					if err := compiled.Validate(sample); err != nil {
						return fmt.Errorf("ezoapi: invalid %s at %s: %w", keyword, path, err)
					}
				}
			}
		}
		if sample, ok := object["default"]; ok {
			if err := compiled.Validate(sample); err != nil {
				return fmt.Errorf("ezoapi: invalid default at %s: %w", path, err)
			}
		}
		if raw, exists := object["discriminator"]; exists {
			data, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			var d Discriminator
			if err := json.Unmarshal(data, &d); err != nil {
				return fmt.Errorf("ezoapi: invalid discriminator: %w", err)
			}
			_, one := object["oneOf"]
			_, anyOf := object["anyOf"]
			_, all := object["allOf"]
			if d.PropertyName == "" || (!one && !anyOf && !all) {
				return fmt.Errorf("ezoapi: discriminator needs propertyName and oneOf/anyOf/allOf at %s", path)
			}
			for _, target := range d.Mapping {
				ref, err := url.Parse(target)
				if err != nil {
					return err
				}
				if target == "" || (!strings.HasPrefix(target, "#") && !ref.IsAbs()) {
					return fmt.Errorf("ezoapi: discriminator mapping needs an explicit local schema reference: %q", target)
				}
				if _, err := c.Compile(base.ResolveReference(ref).String()); err != nil {
					return fmt.Errorf("ezoapi: discriminator mapping %q: %w", target, err)
				}
			}
		}
		for _, keyword := range []string{"$defs", "definitions", "properties", "patternProperties", "dependentSchemas"} {
			if children, ok := object[keyword].(map[string]any); ok {
				for name, child := range children {
					escaped := strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
					if err := walk(child, path+"/"+keyword+"/"+escaped, base); err != nil {
						return err
					}
				}
			}
		}
		for _, keyword := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
			if children, ok := object[keyword].([]any); ok {
				for i, child := range children {
					if err := walk(child, path+"/"+keyword+"/"+strconv.Itoa(i), base); err != nil {
						return err
					}
				}
			}
		}
		for _, keyword := range []string{"items", "additionalProperties", "unevaluatedProperties", "propertyNames", "contains", "unevaluatedItems", "not", "if", "then", "else", "contentSchema"} {
			if child, ok := object[keyword]; ok {
				if err := walk(child, path+"/"+keyword, base); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(doc, "", base)
}

func schemaWireType(s *Schema, defs map[string]*Schema, depth int) string {
	if s == nil || depth > 32 {
		return ""
	}
	if len(s.Defs) > 0 {
		defs = s.Defs
	}
	if s.Ref != "" {
		return schemaWireType(defs[strings.TrimPrefix(s.Ref, "#/$defs/")], defs, depth+1)
	}
	if s.Type != "" {
		return s.Type
	}
	if s.Types != nil {
		kind := ""
		for _, t := range s.Types {
			if t == "null" {
				continue
			}
			if kind != "" && kind != t {
				return ""
			}
			kind = t
		}
		return kind
	}
	kind := ""
	for _, branch := range s.AnyOf {
		t := schemaWireType(branch, defs, depth+1)
		if t == "null" {
			continue
		}
		if t == "" || (kind != "" && kind != t) {
			return ""
		}
		kind = t
	}
	return kind
}
