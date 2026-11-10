# ezoapi

Reusable Go library for Fiber v3 route registration and OpenAPI 3.1 generation.
Module: `github.com/sonseait/ezoapi`. Use Go 1.27.4 (verified against
`https://go.dev/dl/?mode=json` on 2026-11-11). Preserve applicable global rules.

- Root package contains the public schema models (`openapi.go`), JSON Schema
  codec and validation (`schema.go`), reflection, and Fiber adapter.
- `examples/basic` is a runnable consumer, not a production backend service.
- This is a Go module, not a container delivery. Do not add container files or
  build/push images unless explicitly requested for a deployment workflow.
- Keep dependencies limited to Fiber and the JSON Schema validator. Consumer
  services own logging, metrics, traces, configuration, runtime validation,
  dashboards, databases, authentication and response envelopes; do not inject
  these into handlers from this library.
- Registration happens before serving. Preserve Fiber middleware order and group
  prefixes. Return documentation errors before registering a Fiber route.
- Follow encoding/json wire behavior. Never guess types or examples from names.
  Unknown custom JSON marshalers require an explicit schema mapping.
- Preserve exact numeric examples, false, zero, empty values and explicit null.
- Use local schema resources for recursive references. Reject external schema
  loads. Add regression coverage for reflection logic and serialization changes.
- Advanced unions and discriminator mappings are explicit schemas, never guessed
  from interface values. Use `$ref` instead of cyclic Go schema pointers.
- Boolean schemas, type arrays, `const: null`, exact numeric constraints and
  unknown keywords must survive cloning and document snapshots.
- BodyContent/ResponseContent describe media types; handlers retain control of
  binding and writing JSON, forms, files or streams. Preserve JSON byte-slice
  base64 behavior independently from raw binary media.
- Prefer stdlib, small concrete APIs and focused functions. No new abstractions
  or dependencies for functionality already provided by existing code.

Commands (run at project root):

```sh
go mod download
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
go build ./...
go run ./examples/basic
```

Go compilation checks types; there is no separate typecheck or frontend toolchain.
Read README.md for local consumer setup. Add integrations only when the actual
project needs them; keep this library and its example small.
