package ezoapi

import (
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

type node struct {
	Value int   `json:"value"`
	Next  *node `json:"next,omitempty"`
}
type recursiveMap map[string]recursiveMap
type recursiveSlice []recursiveSlice
type recursivePointer *recursivePointer
type page[T any] struct {
	Docs  []T `json:"docs"`
	Count int `json:"count"`
}
type textID [2]byte

func (v textID) MarshalText() ([]byte, error) { return []byte("id"), nil }

type customJSON struct{}
type customString string

func (customString) MarshalJSON() ([]byte, error) { return []byte(`123`), nil }

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`"custom"`), nil }

func TestWireTypes(t *testing.T) {
	stamp := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		dto    any
		values []any
	}{
		{"bool", false, []any{true, false}},
		{"signed", int64(0), []any{int64(math.MinInt64), int64(math.MaxInt64)}},
		{"unsigned", uint64(0), []any{uint64(math.MaxUint64), uint64(0)}},
		{"int8", int8(0), []any{int8(-128), int8(127)}},
		{"float", float32(0), []any{float32(1.25)}},
		{"pointer", (**int)(nil), []any{nil, ptr(0), ptr(ptr(4))}},
		{"time", stamp, []any{stamp}},
		{"bytes", []byte(nil), []any{[]byte(nil), []byte{0, 255}}},
		{"byte array", [2]byte{}, []any{[2]byte{0, 255}}},
		{"array", [2]string{}, []any{[2]string{"a", "b"}}},
		{"slice", []*node{}, []any{[]*node(nil), []*node{nil, {Value: 1}}}},
		{"map", map[int]*node{}, []any{map[int]*node(nil), map[int]*node{12: {Value: 1}}}},
		{"text key", map[textID]int{}, []any{map[textID]int{{}: 3}}},
		{"text value", textID{}, []any{textID{}}},
		{"any", reflect.TypeFor[any](), []any{nil, false, 42, "x", []any{1}, map[string]any{"x": true}}},
		{"raw", json.RawMessage{}, []any{json.RawMessage(`false`), json.RawMessage(`[1,"x"]`), json.RawMessage(`null`)}},
		{"number", json.Number("0"), []any{json.Number("9007199254740993"), json.Number("1.5")}},
		{"recursive", node{}, []any{node{Value: 1, Next: &node{Value: 2}}}},
		{"recursive map", recursiveMap{}, []any{recursiveMap{"a": {"b": nil}}}},
		{"recursive slice", recursiveSlice{}, []any{recursiveSlice{{nil}}}},
		{"recursive pointer", reflect.TypeFor[recursivePointer](), []any{recursivePointer(nil)}},
		{"generics", page[node]{}, []any{page[node]{Docs: []node{{Value: 2}}, Count: 1}}},
		{"quoted", struct {
			N uint64 `json:"n,string"`
			B bool   `json:"b,string"`
			S string `json:"s,string"`
		}{}, []any{struct {
			N uint64 `json:"n,string"`
			B bool   `json:"b,string"`
			S string `json:"s,string"`
		}{math.MaxUint64, false, "a"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := (Reflector{}).Schema(tc.dto)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range tc.values {
				if _, err := ValidateExample(s, v); err != nil {
					t.Fatalf("%#v: %v", v, err)
				}
			}
		})
	}
}

func rootObject(s *Schema) *Schema {
	if s.Ref != "" {
		return s.Defs[strings.TrimPrefix(s.Ref, "#/$defs/")]
	}
	return s
}

func TestJSONFieldSelection(t *testing.T) {
	type A struct {
		Name   string `json:"a"`
		Clash  string
		Tagged string
	}
	type B struct {
		Name  int `json:"b"`
		Clash int
		Other bool `json:"Tagged"`
	}
	type hidden struct {
		Public string `json:"public"`
	}
	type DTO struct {
		A
		B
		*hidden
		Plain   int
		Omit    string    `json:"omit,omitempty"`
		Zero    int       `json:"zero,omitzero"`
		Stamp   time.Time `json:"stamp,omitempty"`
		Fixed   [1]int    `json:"fixed,omitempty"`
		Skip    string    `json:"-"`
		private string
	}
	v := DTO{A: A{Name: "x", Clash: "ignored", Tagged: "ignored"}, B: B{Name: 2, Other: true}}
	s, err := (Reflector{}).Schema(v)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	obj := rootObject(s)
	for name := range actual {
		if obj.Properties[name] == nil {
			t.Errorf("JSON key %q missing from schema", name)
		}
	}
	for _, name := range []string{"Clash", "Skip", "private"} {
		if obj.Properties[name] != nil {
			t.Errorf("unexpected property %q", name)
		}
	}
	for _, name := range []string{"omit", "zero", "public"} {
		if slices.Contains(obj.Required, name) {
			t.Errorf("%q must be optional", name)
		}
	}
	for _, name := range []string{"stamp", "fixed"} {
		if !slices.Contains(obj.Required, name) {
			t.Errorf("%q is always emitted by encoding/json", name)
		}
	}
	if _, err := ValidateExample(s, v); err != nil {
		t.Fatal(err)
	}
	if obj.Properties["Tagged"].Type != "boolean" {
		t.Fatal("tagged field must dominate untagged field")
	}
}

func TestExamples(t *testing.T) {
	type DTO struct {
		ID      string      `json:"id" example:"123"`
		Count   uint64      `json:"count" example:"18446744073709551615"`
		Number  json.Number `json:"number" example:"9007199254740993"`
		Enabled bool        `json:"enabled" example:"false" default:"false"`
		Zero    int         `json:"zero" example:"0"`
		Role    int         `json:"role" enum:"[1,2]" example:"2"`
		Names   []string    `json:"names" example:"[\"a\",\"b\"]" validate:"min=1,max=3"`
		When    time.Time   `json:"when" example:"\"2026-11-11T12:00:00Z\""`
	}
	s, err := (Reflector{}).Schema(DTO{})
	if err != nil {
		t.Fatal(err)
	}
	obj := rootObject(s)
	if obj.Properties["id"].Examples[0] != "123" {
		t.Fatal("string ID must remain a string")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`18446744073709551615`, `"examples":[false]`, `"examples":[0]`} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("missing %s in %s", expected, data)
		}
	}
	bad := []any{
		struct {
			N int `example:"wrong"`
		}{},
		struct {
			N uint8 `example:"256"`
		}{},
		struct {
			N int64 `example:"9223372036854775808"`
		}{},
		struct {
			N bool `example:"0"`
		}{},
		struct {
			N string `enum:"[1,2]"`
		}{},
		struct {
			N int `enum:"[1,2]" example:"3"`
		}{},
		struct {
			N string `validate:"min=3" example:"a"`
		}{},
		struct {
			N time.Time `example:"\"not-a-date\""`
		}{},
	}
	for _, dto := range bad {
		if _, err := (Reflector{}).Schema(dto); err == nil {
			t.Errorf("expected error for %T", dto)
		}
	}
	for _, dto := range []any{struct {
		N uint64 `enum:"[18446744073709551616]"`
	}{}, struct {
		N int64 `enum:"[-9223372036854775809]"`
	}{}, struct {
		N uint64 `validate:"min=9007199254740993" example:"9007199254740992"`
	}{}} {
		if _, err := (Reflector{}).Schema(dto); err == nil {
			t.Errorf("out of range enum/constraint accepted for %T", dto)
		}
	}
	data, err = ValidateExample(&Schema{Type: "integer"}, uint64(math.MaxUint64))
	if err != nil || string(data) != "18446744073709551615" {
		t.Fatalf("precision lost: %s %v", data, err)
	}
}

func TestUnsupportedAndCustomTypes(t *testing.T) {
	for _, dto := range []any{nil, make(chan int), func() {}, complex(1, 2), map[bool]int{}, customJSON{}} {
		if _, err := (Reflector{}).Schema(dto); err == nil {
			t.Errorf("expected error for %T", dto)
		}
	}
	r := Reflector{TypeSchemas: map[reflect.Type]*Schema{reflect.TypeFor[customJSON](): {Type: "string", Enum: []any{"custom"}}}}
	s, err := r.Schema(struct{ Value customJSON }{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, struct{ Value customJSON }{}); err != nil {
		t.Fatal(err)
	}
	r.TypeSchemas[reflect.TypeFor[customString]()] = &Schema{Type: "integer"}
	v := struct {
		Value customString `json:"value,string"`
	}{}
	s, err = r.Schema(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExample(s, v); err != nil {
		t.Fatal(err)
	}
	invalid := Reflector{TypeSchemas: map[reflect.Type]*Schema{reflect.TypeFor[int](): {Type: "integer", Minimum: "invalid"}}}
	if _, err := invalid.Schema(struct {
		N int `validate:"min=1"`
	}{}); err == nil {
		t.Fatal("invalid override bounds accepted")
	}
}

func TestRoutes(t *testing.T) {
	app := fiber.New()
	r := New(app, Config{Info: Info{Title: "Test", Version: "1"}})
	var order []string
	middleware := func(name string) fiber.Handler {
		return func(c fiber.Ctx) error { order = append(order, name); return c.Next() }
	}
	group := r.Group("/api", middleware("group")).Group("/v1")
	err := group.Post("/users/:id", func(c fiber.Ctx) error {
		order = append(order, "handler")
		return c.Status(201).JSON(fiber.Map{"id": "42"})
	}, Route{
		Middleware: []fiber.Handler{middleware("route")}, Status: 201,
		Params: struct {
			ID string `uri:"id" example:"42"`
		}{},
		Query: struct {
			Limit int    `query:"limit" validate:"required,min=1,max=100" example:"10"`
			Skip  string `query:"-"`
		}{},
		Body: struct {
			Name string `json:"name"`
		}{},
		Response: struct {
			ID string `json:"id"`
		}{}, ResponseExample: map[string]string{"id": "42"},
		Responses: map[int]ResponseSpec{400: {DTO: struct {
			Error string `json:"error"`
		}{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := app.Test(httptest.NewRequest("POST", "/api/v1/users/42?limit=10", strings.NewReader(`{"name":"Ada"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 || !reflect.DeepEqual(order, []string{"group", "route", "handler"}) {
		t.Fatalf("status %d order %v", response.StatusCode, order)
	}
	app.Get("/openapi.json", r.Handler())
	response, err = app.Test(httptest.NewRequest("GET", "/openapi.json", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/api/v1/users/{id}"]["post"]
	if doc.OpenAPI != "3.1.0" || op == nil || op.Responses["201"].Content == nil || len(op.Parameters) != 2 {
		t.Fatalf("wrong document: %s", data)
	}
	for _, p := range op.Parameters {
		if !p.Required {
			t.Errorf("parameter %s should be required", p.Name)
		}
	}
	media := op.Responses["201"].Content["application/json"]
	if _, err := ValidateExample(media.Schema, map[string]string{"id": "42"}); err != nil {
		t.Fatalf("embedded schema refs: %v", err)
	}
	snapshot, err := r.Document()
	if err != nil {
		t.Fatal(err)
	}
	delete(snapshot.Paths, "/api/v1/users/{id}")
	again, err := r.Document()
	if err != nil || len(again.Paths) != 1 {
		t.Fatal("snapshot mutated live document")
	}
}

func TestRejectBeforeFiberRegistration(t *testing.T) {
	app := fiber.New()
	r := New(app, Config{})
	handler := func(c fiber.Ctx) error { return c.SendStatus(200) }
	for _, tc := range []struct {
		method, path string
		cfg          Route
	}{
		{"CONNECT", "/x", Route{}}, {"QUERY", "/x", Route{}}, {"GET", "/x/:id?", Route{}}, {"GET", "/x/*", Route{}}, {"GET", "/x/:id<int>", Route{}},
		{"GET", "/x", Route{Response: customJSON{}}}, {"POST", "/x", Route{Body: struct {
			N int `example:"bad"`
		}{}}},
		{"DELETE", "/x", Route{Status: 204, Response: "body"}}, {"HEAD", "/x", Route{Response: "body"}},
		{"GET", "/x", Route{Response: int(0), ResponseExample: "wrong"}},
	} {
		if err := r.Add(tc.method, tc.path, handler, tc.cfg); err == nil {
			t.Errorf("expected error for %s %s", tc.method, tc.path)
		}
	}
	if len(app.GetRoutes()) != 0 {
		t.Fatal("invalid docs registered Fiber routes")
	}
	if err := r.Get("/users/:id", handler); err != nil {
		t.Fatal(err)
	}
	if err := r.Get("/users/:name", handler); err == nil {
		t.Fatal("ambiguous templates accepted")
	}
	if err := r.Get("/users/:id", handler); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Head("/health", handler); err != nil {
		t.Fatal(err)
	}
	if err := r.Options("/health", handler); err != nil {
		t.Fatal(err)
	}
	if err := r.Trace("/health", handler); err != nil {
		t.Fatal(err)
	}
	existing := New(app.Group("/existing"), Config{})
	if err := existing.Get("/route", handler); err != nil {
		t.Fatal(err)
	}
	doc, err := existing.Document()
	if err != nil || doc.Paths["/existing/route"] == nil {
		t.Fatal("existing group prefix lost")
	}
	if err := existing.Get("", handler); err != nil {
		t.Fatal(err)
	}
	doc, err = existing.Document()
	if err != nil || doc.Paths["/existing"] == nil {
		t.Fatal("empty route path should match Fiber group prefix")
	}
}

func TestSecurityParametersAndSnapshotPrecision(t *testing.T) {
	app := fiber.New()
	r := New(app, Config{SecuritySchemes: map[string]SecurityScheme{"bearer": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"}}})
	handler := func(c fiber.Ctx) error { return c.SendStatus(200) }
	if err := r.Get("/private", handler, Route{
		Security: []map[string][]string{{"bearer": {}}},
		Query: struct {
			Tags []string  `query:"tags"`
			At   time.Time `query:"at"`
		}{},
		Headers: struct {
			Trace string `header:"X-Trace"`
		}{},
		Cookies: struct {
			Session string `cookie:"session"`
		}{},
		Response: reflect.TypeFor[uint64](), ResponseExample: uint64(math.MaxUint64),
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := r.Document()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"example":18446744073709551615`) || !strings.Contains(string(data), `"maximum":18446744073709551615`) {
		t.Fatalf("snapshot rounded number: %s", data)
	}
	if len(doc.Paths["/private"]["get"].Parameters) != 4 {
		t.Fatal("missing parameters")
	}
	for _, cfg := range []Route{
		{Security: []map[string][]string{{"missing": {}}}},
		{Security: []map[string][]string{{"bearer": nil}}},
		{Query: struct {
			Nested []node `query:"nested"`
		}{}},
		{Headers: struct {
			Auth string `header:"Authorization"`
		}{}},
		{Headers: struct {
			A string `header:"X-ID"`
			B string `header:"x-id"`
		}{}},
	} {
		if err := r.Get("/invalid", handler, cfg); err == nil {
			t.Errorf("accepted invalid config: %+v", cfg)
		}
	}
}
