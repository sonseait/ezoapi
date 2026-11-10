# API guide

ezoapi registers Fiber v3 routes and builds OpenAPI 3.1 documentation. See the
[README](../README.md) for installation and a complete starting example.

## Routes and DTOs

`Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Trace`, and
`Add(method, path, handler, route...)` return an error. Handle errors during
startup: documentation is checked before the Fiber route is registered.

`Group` supports nested prefixes and middleware. `Use` delegates to Fiber, and
`New` can wrap an existing Fiber group. Route middleware runs before the handler,
after group middleware. Finish registration before serving requests; do not mutate
schema, configuration, or middleware metadata afterward. `Document()` returns an
independent snapshot; `Handler()` serves the specification as JSON.

`Route.Body`, `Query`, `Params`, `Headers`, `Cookies`, and `Response` accept a
value, a typed nil pointer, or `reflect.TypeFor[T]()`. DTO values identify types;
provide payload examples separately with `BodyExample` and `ResponseExample`.
`Body` and `Response` also accept `Schema` or `*Schema`.

| Input | Field-name tag | Required by default |
| --- | --- | --- |
| Path (`Params`) | `uri` | Yes |
| Query (`Query`) | `query` | No |
| Header (`Headers`) | `header` | No |
| Cookie (`Cookies`) | `cookie` | No |

Parameter names fall back to `json`, then the Go field name. A `"-"` tag omits a
field. Use `required:"true"` or the `validate:"required"` token to require an
optional parameter. Parameters support scalars and arrays of scalars; object
parameter serialization is not implemented.

Fiber paths such as `:id` become OpenAPI `{id}` parameters. Missing parameter
metadata defaults to a required string. Wildcards, optional or constrained
parameters, and compound parameters within one segment return errors; register
concrete routes instead. Duplicate routes, duplicate operation IDs, and templates
that differ only in parameter names also return errors. CONNECT and QUERY are
not supported by OpenAPI 3.1.

Bodies are required unless `BodyOptional: true` is set. The default media type is
`application/json`. Responses default to status 200; set `Status` to change it.
Omitting `Response` documents no response body. Use
`Responses: map[int]ezoapi.ResponseSpec{...}` for additional status codes, DTOs,
and examples. Response bodies are rejected for HEAD and statuses 1xx/204/205/304.

## JSON types, tags, and examples

Reflection follows `encoding/json`: exported fields, renaming and omission,
embedded-field dominance, `omitempty`, `omitzero`, `,string`, named types, and
generics. Fields without a `json` tag use their Go names. Fields without an omit
option are marked required according to their marshaled JSON shape.

Supported types include booleans, signed and unsigned integers, floats, strings,
arrays, slices, structs, pointers, interfaces, recursive types, and maps with
JSON-compatible string, integer, or TextMarshaler keys. Pointers, slices, and maps
allow null; arrays have fixed lengths. Special representations include:

| Go type | JSON schema representation |
| --- | --- |
| `[]byte` | Base64 string |
| `[N]byte` | Fixed-length array |
| `time.Time` | Date-time string |
| `json.RawMessage`, interface | Any JSON value |
| `json.Number` | Number |

Schemas use local `$id` and `$defs` resources to keep recursive references valid
when embedded in OpenAPI. External schema loading is rejected.

```go
type Input struct {
	ID      string   `json:"id" example:"123"`
	Count   uint64   `json:"count" example:"18446744073709551615"`
	Enabled bool     `json:"enabled" example:"false" default:"false"`
	Roles   []string `json:"roles" example:"[\"admin\"]"`
	State   int      `json:"state" enum:"[1,2]" example:"2"`
	Name    string   `json:"name" description:"Display name" validate:"min=2,max=50"`
}
```

`example`, `default`, and `enum` use JSON; string examples and defaults also
accept plain text. Enums must be JSON arrays and preserve each element's type.
Examples are never inferred from field names. Registration catches examples and
defaults with invalid types, values outside Go type bounds, or schema violations.

`BodyExample` and `ResponseExample` are marshaled with `encoding/json`, checked
against JSON Schema, and preserved as JSON, including large numbers, `false`,
`0`, `[]`, `{}`, and `""`. Use `json.RawMessage("null")` for explicit null.

Supported `validate` rules are `required`, `min`, `max`, `len`, numeric
`gte/lte/gt/lt`, string/integer `oneof`, array `unique`, and formats `email`,
`uuid`, `uuid4`, `url`/`uri`, `ipv4`, `ipv6`, and `hostname`. Integer `oneof`
preserves uint64 values. Quote strings containing spaces, as in
`oneof='red blue' green`. Translation stops at `dive`; conditional and custom
rules are not inferred. Use `format` to set a JSON Schema format; standard formats
and base64 are checked in examples. These tags document constraints; handlers
remain responsible for runtime binding and validation.

64-bit integer bounds retain exact JSON numbers. `json:",string"` uses
`contentMediaType` and `contentSchema` to check the JSON inside the string,
including integer bounds and supported rules. Byte-length constraints before
base64 encoding are not translated to base64 string lengths.

## Custom wire representations

Custom `json.Marshaler` types need an explicit schema mapping because reflection
cannot determine what their code emits. A value-receiver `encoding.TextMarshaler`
is represented as a string. For a custom type `Decimal`, configure its actual
wire representation before registering routes:

```go
reflector := ezoapi.Reflector{
	TypeSchemas: map[reflect.Type]*ezoapi.Schema{
		reflect.TypeFor[Decimal](): {Type: "string", Pattern: `^-?[0-9]+(\.[0-9]+)?$`},
	},
}
api := ezoapi.New(app, ezoapi.Config{Reflector: reflector})
```

Mappings must match the application's encoder. Functions, channels, complex
numbers, incompatible map keys, and unsupported types return errors. Applications
with custom JSON encoders must supply corresponding mappings.

## Explicit schemas

`Schema` supports type unions (`Types`), `oneOf`/`anyOf`/`allOf`, discriminators,
`const`, `not`, `if`/`then`/`else`, exclusive bounds, `multipleOf`, tuples,
`contains`, `uniqueItems`, pattern and property-name constraints, dependencies,
and `unevaluatedProperties`/`unevaluatedItems`.

```go
tuple := &ezoapi.Schema{
	Type:        "array",
	PrefixItems: []*ezoapi.Schema{{Type: "string"}, {Type: "integer"}},
	Items:       ezoapi.BoolSchema(false),
}
nullableString := &ezoapi.Schema{Types: []string{"string", "null"}}
closedObject := &ezoapi.Schema{
	Type:                 "object",
	Properties:           map[string]*ezoapi.Schema{"name": {Type: "string"}},
	AdditionalProperties: ezoapi.BoolSchema(false),
}
```

`BoolSchema(true)` accepts every value; `BoolSchema(false)` accepts none. `Type`
and `Types` are mutually exclusive. `Const` and `Default` use `json.RawMessage`
to represent explicit null, false, and zero. Numeric bounds and `MultipleOf`
accept Go numbers or `json.Number` for exact large numbers and decimals.

Unions are explicit, not inferred from interface values. Define variants in
`Defs`, reference them in `OneOf`, and set `Discriminator.PropertyName` and
`Mapping`, such as `"cat": "#/$defs/Cat"`. Each variant must constrain the
discriminating field with `const`/`enum` and `required`; the discriminator itself
is tooling metadata. Mappings must reference local schemas and are checked during
registration. See [advanced_test.go](../advanced_test.go) for complete examples.

Override a field with a JSON Schema tag:

```go
type Event struct {
	Value  any    `json:"value" schema:"{\"type\":[\"string\",\"integer\"]}" example:"0"`
	Secret string `json:"secret" schema:"{\"type\":\"string\",\"writeOnly\":true}"`
}
```

Use `Schema.Extra` for keywords without a Go field, such as
`Extra: map[string]any{"x-unit": "USD"}`. Unknown keywords survive serialization
and document snapshots; `Extra` cannot overwrite typed keywords. Schemas are
cloned before use, and structure, examples, and defaults are checked throughout
branches and `$defs`. Use `$ref` for recursion instead of cyclic Go pointers.

## Forms, files, and other media types

```go
ezoapi.Route{
	BodyContent: map[string]ezoapi.MediaType{
		"multipart/form-data": {
			Schema: &ezoapi.Schema{
				Type: "object",
				Properties: map[string]*ezoapi.Schema{
					"file": {Type: "string", Format: "binary"},
					"meta": {Type: "object"},
				},
			},
			Encoding: map[string]ezoapi.Encoding{
				"meta": {ContentType: "application/json"},
			},
		},
	},
	ResponseContent: map[string]ezoapi.MediaType{
		"text/plain":               {Schema: &ezoapi.Schema{Type: "string"}, Example: "ok"},
		"application/octet-stream": {},
	},
}
```

The same API describes URL-encoded forms, event streams, custom media types, and
JSON schema overrides. `Encoding` applies only to forms and multipart content.
`BodyContent` is mutually exclusive with `Body`/`BodyExample`; `ResponseContent`
and `ResponseSpec.Content` are mutually exclusive with their corresponding
response DTO/example. Handlers bind forms and files and write responses or
streams using Fiber. JSON `[]byte` remains base64, independently of raw binary
media types.

## Authentication and application envelopes

Configure HTTP or API key schemes through `Config.SecuritySchemes`, and reference
them with `Route.Security`, for example
`[]map[string][]string{{"bearerAuth": {}}}`. Authentication is enforced by your
own middleware.

Response envelopes and pagination are application types. Generic types are
reflected directly:

```go
type Envelope[T any] struct {
	Code      string `json:"code" example:"ok"`
	RequestID string `json:"requestId"`
	Data      T      `json:"data"`
}
// Response: reflect.TypeFor[Envelope[User]]()
```

When migrating from PGW, replace `BodyDTO/QueryDTO/ResponseDTO` with
`Body/Query/Response` and `Middlewares` with `Middleware`. Generics remove the
need for `ItemDTO`. ezoapi leaves envelopes, JWT, pagination, and operational
integrations to the consuming application.

## Local development

Run from the repository root with Go 1.27.4:

```sh
go mod download
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
go build ./...
go run ./examples/basic
```

Go compilation checks types; there is no separate type checker or frontend build.
To use a local checkout from another module:

```sh
go mod edit -replace github.com/sonseait/ezoapi=../ezoapi
go get github.com/sonseait/ezoapi
```

Adjust the relative checkout path as needed. Use
`go get github.com/sonseait/ezoapi@<tag>` when selecting a released version.
