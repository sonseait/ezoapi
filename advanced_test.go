package ezoapi

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestAdvancedSchemas(t *testing.T) {
	cases := []struct {
		name           string
		schema         *Schema
		valid, invalid []any
	}{
		{"nullable union", &Schema{Types: []string{"string", "null"}}, []any{"x", nil}, []any{1, false}},
		{"accept all", BoolSchema(true), []any{nil, false, 0, []any{}, map[string]any{}}, nil},
		{"reject all", BoolSchema(false), nil, []any{nil, false, 0}},
		{"const null", &Schema{Const: json.RawMessage(`null`)}, []any{nil}, []any{false, 0}},
		{"const false", &Schema{Const: json.RawMessage(`false`)}, []any{false}, []any{nil, true}},
		{"numeric bounds", &Schema{Type: "number", ExclusiveMinimum: 0, ExclusiveMaximum: 1, MultipleOf: json.Number("0.1")}, []any{json.Number("0.3")}, []any{0, 1, json.Number("0.31")}},
		{"exact integer bound", &Schema{Type: "integer", Minimum: json.Number("9007199254740993"), Maximum: uint64(math.MaxUint64)}, []any{uint64(math.MaxUint64)}, []any{json.Number("9007199254740992"), json.Number("18446744073709551616")}},
		{"closed object", &Schema{Type: "object", Properties: map[string]*Schema{"id": {Type: "integer"}}, Required: []string{"id"}, AdditionalProperties: BoolSchema(false)}, []any{map[string]any{"id": 0}}, []any{map[string]any{"id": 0, "extra": true}}},
		{"property constraints", &Schema{Type: "object", PatternProperties: map[string]*Schema{"^x-": {Type: "integer"}}, PropertyNames: &Schema{Pattern: "^x-"}, AdditionalProperties: BoolSchema(false)}, []any{map[string]any{"x-id": 2}}, []any{map[string]any{"x-id": "bad"}, map[string]any{"id": 2}}},
		{"tuple", &Schema{Type: "array", PrefixItems: []*Schema{{Type: "string"}, {Type: "integer"}}, Items: BoolSchema(false), MinItems: ptr(2)}, []any{[]any{"x", 1}}, []any{[]any{"x"}, []any{"x", 1, true}, []any{1, "x"}}},
		{"contains unique", &Schema{Type: "array", Items: &Schema{Type: "integer"}, Contains: &Schema{Const: json.RawMessage(`0`)}, MinContains: ptr(1), MaxContains: ptr(1), UniqueItems: true}, []any{[]any{0, 1}}, []any{[]any{1}, []any{0, 0}, []any{0, 1, 1}}},
		{"not", &Schema{Type: "string", Not: &Schema{Const: json.RawMessage(`"blocked"`)}}, []any{"allowed"}, []any{"blocked"}},
		{"dependent required", &Schema{Type: "object", DependentRequired: map[string][]string{"card": {"billing"}}}, []any{map[string]any{"card": "x", "billing": "y"}}, []any{map[string]any{"card": "x"}}},
		{"dependent schema", &Schema{Type: "object", DependentSchemas: map[string]*Schema{"card": {Required: []string{"billing"}}}}, []any{map[string]any{}}, []any{map[string]any{"card": "x"}}},
		{"conditional", &Schema{Type: "object", Required: []string{"kind"}, If: &Schema{Properties: map[string]*Schema{"kind": {Const: json.RawMessage(`"card"`)}}}, Then: &Schema{Required: []string{"number"}}, Else: &Schema{Not: &Schema{Required: []string{"number"}}}}, []any{map[string]any{"kind": "card", "number": "123"}, map[string]any{"kind": "cash"}}, []any{map[string]any{"kind": "card"}, map[string]any{"kind": "cash", "number": "123"}}},
		{"unevaluated properties", &Schema{AllOf: []*Schema{{Properties: map[string]*Schema{"a": {Type: "integer"}}}, {Properties: map[string]*Schema{"b": {Type: "string"}}}}, UnevaluatedProperties: BoolSchema(false)}, []any{map[string]any{"a": 1, "b": "x"}}, []any{map[string]any{"c": true}}},
		{"unevaluated items", &Schema{Type: "array", PrefixItems: []*Schema{{Type: "integer"}}, UnevaluatedItems: BoolSchema(false)}, []any{[]any{1}}, []any{[]any{1, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := (Reflector{}).Schema(tc.schema)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range tc.valid {
				if _, err := ValidateExample(s, v); err != nil {
					t.Fatalf("valid %#v rejected: %v", v, err)
				}
			}
			for _, v := range tc.invalid {
				if _, err := ValidateExample(s, v); err == nil {
					t.Fatalf("invalid %#v accepted", v)
				}
			}
		})
	}
}

func petSchema() *Schema {
	return &Schema{
		Defs: map[string]*Schema{
			"Cat": {Type: "object", Properties: map[string]*Schema{"kind": {Const: json.RawMessage(`"cat"`)}, "lives": {Type: "integer"}}, Required: []string{"kind", "lives"}},
			"Dog": {Type: "object", Properties: map[string]*Schema{"kind": {Const: json.RawMessage(`"dog"`)}, "good": {Type: "boolean"}}, Required: []string{"kind", "good"}},
		},
		OneOf:         []*Schema{{Ref: "#/$defs/Cat"}, {Ref: "#/$defs/Dog"}},
		Discriminator: &Discriminator{PropertyName: "kind", Mapping: map[string]string{"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}},
	}
}

func TestDiscriminatorAndReferenceScopes(t *testing.T) {
	pet := petSchema()
	pet.Examples = []any{map[string]any{"kind": "cat", "lives": 9}, map[string]any{"kind": "dog", "good": false}}
	s, err := (Reflector{}).Schema(pet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, map[string]any{"kind": "bird"}); err == nil {
		t.Fatal("unknown variant accepted")
	}
	if pet.ID != "" {
		t.Fatal("caller schema mutated")
	}
	for _, broken := range []*Schema{
		{OneOf: []*Schema{{Type: "string"}}, Discriminator: &Discriminator{}},
		{Discriminator: &Discriminator{PropertyName: "kind"}},
	} {
		if _, err := (Reflector{}).Schema(broken); err == nil {
			t.Fatal("invalid discriminator accepted")
		}
	}
	pet = petSchema()
	pet.Discriminator.Mapping["cat"] = "#/$defs/Missing"
	if _, err := (Reflector{}).Schema(pet); err == nil {
		t.Fatal("dangling mapping accepted")
	}
	pet = petSchema()
	pet.Discriminator.Mapping["cat"] = "https://example.invalid/Cat"
	if _, err := (Reflector{}).Schema(pet); err == nil {
		t.Fatal("external mapping accepted")
	}
	// A mapping can own local definitions, then be reused on multiple DTO fields.
	type A map[string]any
	type B map[string]any
	r := Reflector{TypeSchemas: map[reflect.Type]*Schema{reflect.TypeFor[A](): petSchema(), reflect.TypeFor[B](): petSchema()}}
	s, err = r.Schema(struct {
		A A
		B B
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, struct {
		A A
		B B
	}{A{"kind": "cat", "lives": 9}, B{"kind": "dog", "good": false}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reflector{}).Schema(&Schema{Defs: map[string]*Schema{"Unused": {Type: "integer", Examples: []any{"wrong"}}}}); err == nil {
		t.Fatal("invalid example in unused definition accepted")
	}
}

func TestSchemaRoundTrip(t *testing.T) {
	raw := `{"type":["integer","null"],"maximum":18446744073709551615,"const":null,"additionalProperties":false,"x-large":18446744073709551615,"$comment":"kept"}`
	var s Schema
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"type":["integer","null"]`, `"additionalProperties":false`, `"const":null`, `"x-large":18446744073709551615`, `"maximum":18446744073709551615`, `"$comment":"kept"`} {
		if !strings.Contains(string(data), value) {
			t.Fatalf("lost %s in %s", value, data)
		}
	}
	for _, raw := range []string{"true", "false"} {
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(s)
		if err != nil || string(data) != raw {
			t.Fatalf("boolean round trip: %s %v", data, err)
		}
	}
	for _, bad := range []*Schema{
		{Type: "string", Types: []string{"null"}},
		{Boolean: ptr(false), Type: "string"},
		{Extra: map[string]any{"type": "string"}},
		{MultipleOf: 0},
		{Types: []string{}},
		{OneOf: []*Schema{}},
	} {
		if _, err := (Reflector{}).Schema(bad); err == nil {
			t.Errorf("invalid schema accepted: %#v", bad)
		}
	}
	cyclic := &Schema{Type: "array"}
	cyclic.Items = cyclic
	if _, err := (Reflector{}).Schema(cyclic); err == nil {
		t.Fatal("cyclic pointers accepted")
	}
	emptyEnum := &Schema{Enum: []any{}}
	data, err = json.Marshal(emptyEnum)
	if err != nil || !strings.Contains(string(data), `"enum":[]`) {
		t.Fatalf("empty enum lost: %s %v", data, err)
	}
}

func TestAdvancedTags(t *testing.T) {
	type DTO struct {
		Variant  any              `json:"variant" schema:"{\"type\":[\"string\",\"integer\"]}" example:"0"`
		State    uint64           `json:"state" validate:"oneof=0 18446744073709551615" example:"18446744073709551615"`
		Name     string           `json:"name" validate:"oneof='red blue' green" example:"red blue"`
		Price    float64          `json:"price" validate:"gt=0,lt=1" example:"0.3"`
		Email    string           `json:"email" validate:"email" example:"user@example.com"`
		IDs      []int            `json:"ids" validate:"unique" example:"[1,2]"`
		Password string           `json:"password" schema:"{\"type\":\"string\",\"writeOnly\":true}"`
		Next     recursivePointer `json:"next" example:"null"`
	}
	s, err := (Reflector{}).Schema(DTO{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, DTO{Variant: 0, State: math.MaxUint64, Name: "red blue", Price: 0.3, Email: "user@example.com", IDs: []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{
		struct {
			N int `validate:"oneof=1 2" example:"3"`
		}{},
		struct {
			N uint8 `validate:"oneof=256"`
		}{},
		struct {
			N string `validate:"email" example:"invalid"`
		}{},
		struct {
			N float64 `validate:"gt=0" example:"0"`
		}{},
		struct {
			N []int `validate:"unique" example:"[1,1]"`
		}{},
		struct {
			N any `schema:"{\"type\":[\"string\",\"integer\"]}" example:"false"`
		}{},
		struct {
			N any `schema:"false" example:"null"`
		}{},
	} {
		if _, err := (Reflector{}).Schema(bad); err == nil {
			t.Errorf("invalid tags accepted: %T", bad)
		}
	}
}

func TestQuotedPrimitiveContent(t *testing.T) {
	type Quoted struct {
		N uint64 `json:"n,string"`
		B bool   `json:"b,string"`
		S string `json:"s,string"`
	}
	s, err := (Reflector{}).Schema(Quoted{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, Quoted{N: math.MaxUint64, B: false, S: "hello"}); err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"not-a-number", "18446744073709551616", "null"} {
		if _, err := ValidateExample(s, map[string]any{"n": number, "b": "false", "s": `"hello"`}); err == nil {
			t.Errorf("invalid encoded integer accepted: %s", number)
		}
	}
	if _, err := (Reflector{}).Schema(struct {
		N uint64 `json:"n,string" validate:"oneof=1 2" example:"3"`
	}{}); err == nil {
		t.Fatal("encoded oneof constraint ignored")
	}
}

func TestAdvancedRouteContent(t *testing.T) {
	app := fiber.New()
	r := New(app, Config{})
	handler := func(c fiber.Ctx) error { return c.SendString("ok") }
	if err := r.Post("/pets", handler, Route{Body: petSchema(), Response: petSchema(), ResponseExample: map[string]any{"kind": "dog", "good": false}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Post("/upload", handler, Route{BodyContent: map[string]MediaType{
		"multipart/form-data":      {Schema: &Schema{Type: "object", Properties: map[string]*Schema{"file": {Type: "string", Format: "binary"}, "meta": {Type: "object"}}}, Encoding: map[string]Encoding{"meta": {ContentType: "application/json"}}},
		"application/octet-stream": {},
	}, ResponseContent: map[string]MediaType{"text/plain": {Schema: &Schema{Type: "string"}, Example: "ok"}, "text/event-stream": {Schema: &Schema{Type: "string"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Post("/null", handler, Route{BodyContent: map[string]MediaType{"application/json": {Schema: &Schema{Types: []string{"string", "null"}}, Example: json.RawMessage(`null`)}}, Response: BoolSchema(true), ResponseExample: json.RawMessage(`null`)}); err != nil {
		t.Fatal(err)
	}
	app.Get("/openapi.json", r.Handler())
	response, err := app.Test(httptest.NewRequest("GET", "/openapi.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var doc Document
	if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"example":null`) || !strings.Contains(string(data), `"contentType":"application/json"`) {
		t.Fatalf("metadata lost: %s", data)
	}
	snapshot, err := r.Document()
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(data), `"example":null`) {
		t.Fatalf("null example lost in snapshot: %s %v", data, err)
	}
	for _, bad := range []Route{
		{Body: "x", BodyContent: map[string]MediaType{"text/plain": {}}},
		{BodyContent: map[string]MediaType{}},
		{Response: "x", ResponseContent: map[string]MediaType{"text/plain": {}}},
		{BodyContent: map[string]MediaType{"invalid": {}}},
		{BodyContent: map[string]MediaType{"application/json": {Schema: &Schema{Type: "string"}, Example: false}}},
		{BodyContent: map[string]MediaType{"application/json": {Encoding: map[string]Encoding{"x": {ContentType: "application/json"}}}}},
		{Status: 204, ResponseContent: map[string]MediaType{"text/plain": {}}},
	} {
		before := len(app.GetRoutes())
		if err := r.Post("/invalid", handler, bad); err == nil {
			t.Errorf("bad route accepted: %+v", bad)
		}
		if len(app.GetRoutes()) != before {
			t.Fatal("bad route registered")
		}
	}
}
