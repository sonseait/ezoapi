// Package ezoapi registers Fiber v3 routes and derives OpenAPI 3.1 schemas
// from their DTOs. Documentation does not change request binding or validation.
package ezoapi

import "encoding/json"

// Schema is a JSON Schema 2020-12 schema, as used by OpenAPI 3.1.
type Schema struct {
	Boolean               *bool               `json:"-"`
	ID                    string              `json:"$id,omitempty"`
	Anchor                string              `json:"$anchor,omitempty"`
	Ref                   string              `json:"$ref,omitempty"`
	Defs                  map[string]*Schema  `json:"$defs,omitempty"`
	Type                  string              `json:"type,omitempty"`
	Types                 []string            `json:"-"`
	Title                 string              `json:"title,omitempty"`
	Format                string              `json:"format,omitempty"`
	Description           string              `json:"description,omitempty"`
	Properties            map[string]*Schema  `json:"properties,omitempty"`
	Required              []string            `json:"required,omitempty"`
	Items                 *Schema             `json:"items,omitempty"`
	AdditionalProperties  *Schema             `json:"additionalProperties,omitempty"`
	PatternProperties     map[string]*Schema  `json:"patternProperties,omitempty"`
	PropertyNames         *Schema             `json:"propertyNames,omitempty"`
	UnevaluatedProperties *Schema             `json:"unevaluatedProperties,omitempty"`
	DependentRequired     map[string][]string `json:"dependentRequired,omitempty"`
	DependentSchemas      map[string]*Schema  `json:"dependentSchemas,omitempty"`
	AnyOf                 []*Schema           `json:"anyOf,omitempty"`
	OneOf                 []*Schema           `json:"oneOf,omitempty"`
	AllOf                 []*Schema           `json:"allOf,omitempty"`
	Not                   *Schema             `json:"not,omitempty"`
	If                    *Schema             `json:"if,omitempty"`
	Then                  *Schema             `json:"then,omitempty"`
	Else                  *Schema             `json:"else,omitempty"`
	Const                 json.RawMessage     `json:"const,omitempty"`
	Enum                  []any               `json:"enum,omitempty"`
	Examples              []any               `json:"examples,omitempty"`
	Default               json.RawMessage     `json:"default,omitempty"`
	Minimum               any                 `json:"minimum,omitempty"`
	Maximum               any                 `json:"maximum,omitempty"`
	ExclusiveMinimum      any                 `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum      any                 `json:"exclusiveMaximum,omitempty"`
	MultipleOf            any                 `json:"multipleOf,omitempty"`
	MinLength             *int                `json:"minLength,omitempty"`
	MaxLength             *int                `json:"maxLength,omitempty"`
	MinItems              *int                `json:"minItems,omitempty"`
	MaxItems              *int                `json:"maxItems,omitempty"`
	UniqueItems           bool                `json:"uniqueItems,omitempty"`
	PrefixItems           []*Schema           `json:"prefixItems,omitempty"`
	Contains              *Schema             `json:"contains,omitempty"`
	MinContains           *int                `json:"minContains,omitempty"`
	MaxContains           *int                `json:"maxContains,omitempty"`
	UnevaluatedItems      *Schema             `json:"unevaluatedItems,omitempty"`
	MinProperties         *int                `json:"minProperties,omitempty"`
	MaxProperties         *int                `json:"maxProperties,omitempty"`
	Pattern               string              `json:"pattern,omitempty"`
	ContentEncoding       string              `json:"contentEncoding,omitempty"`
	ContentMediaType      string              `json:"contentMediaType,omitempty"`
	ContentSchema         *Schema             `json:"contentSchema,omitempty"`
	Discriminator         *Discriminator      `json:"discriminator,omitempty"`
	ReadOnly              bool                `json:"readOnly,omitempty"`
	WriteOnly             bool                `json:"writeOnly,omitempty"`
	Deprecated            bool                `json:"deprecated,omitempty"`
	Extra                 map[string]any      `json:"-"`
}

type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type Document struct {
	OpenAPI    string                           `json:"openapi"`
	Info       Info                             `json:"info"`
	Servers    []Server                         `json:"servers,omitempty"`
	Paths      map[string]map[string]*Operation `json:"paths"`
	Components *Components                      `json:"components,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}
type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Name         string `json:"name,omitempty"`
	In           string `json:"in,omitempty"`
}

type Operation struct {
	OperationID string                `json:"operationId,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema"`
}

type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema   *Schema             `json:"schema,omitempty"`
	Example  any                 `json:"example,omitempty"`
	Encoding map[string]Encoding `json:"encoding,omitempty"`
}

func (m *MediaType) UnmarshalJSON(data []byte) error {
	type mediaJSON MediaType
	var decoded mediaJSON
	wrapper := struct {
		*mediaJSON
		Example json.RawMessage `json:"example"`
	}{mediaJSON: &decoded}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	if wrapper.Example != nil {
		decoded.Example = wrapper.Example
	}
	*m = MediaType(decoded)
	return nil
}

type Encoding struct {
	ContentType   string `json:"contentType,omitempty"`
	Style         string `json:"style,omitempty"`
	Explode       *bool  `json:"explode,omitempty"`
	AllowReserved bool   `json:"allowReserved,omitempty"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}
