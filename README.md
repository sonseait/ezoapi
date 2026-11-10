# ezoapi

**Fiber routes. Go types. OpenAPI 3.1.**

ezoapi is a Go library that registers Fiber v3 routes and generates OpenAPI 3.1
documentation from your request and response types. Define an endpoint, attach
its DTOs, and serve an API specification from the same application.

Keep route definitions and API documentation together while using familiar Fiber
handlers, middleware, and groups.

## Why ezoapi?

- **Document APIs where you build them.** Describe parameters, request bodies,
  responses, examples, and security requirements alongside each route.
- **Reuse your Go types.** Generate schemas from structs, generics, embedded
  fields, and recursive types, following `encoding/json` field behavior.
- **Catch documentation errors at startup.** Check schemas and examples before
  registering a route, including duplicate routes and operation IDs.
- **Keep examples accurate.** Preserve large integers, `false`, zero, empty
  values, and explicit JSON `null` without guessing examples from field names.
- **Describe more than JSON.** Document forms, file uploads, binary responses,
  and streams with explicit media types.
- **Use your preferred API tools.** Serve an OpenAPI 3.1 document for tools such
  as Swagger UI or Scalar. Add your own documentation UI when needed.

ezoapi handles route registration and documentation. Your application controls
request binding, runtime validation, authentication, and response formats.

## Install

Requires **Go 1.27.4 or later** and **Fiber v3**. The module currently uses Fiber
v3.6.1.

```sh
go get github.com/sonseait/ezoapi@latest
```

## Your first documented endpoint

```go
package main

import (
	"log"
	"reflect"

	"github.com/gofiber/fiber/v3"
	"github.com/sonseait/ezoapi"
)

type User struct {
	ID   string `json:"id" example:"123"`
	Name string `json:"name" example:"Ada"`
}

func main() {
	app := fiber.New()
	api := ezoapi.New(app, ezoapi.Config{
		Info: ezoapi.Info{Title: "Users API", Version: "1.0.0"},
	})

	err := api.Group("/api/v1").Get("/users/:id", func(c fiber.Ctx) error {
		return c.JSON(User{ID: c.Params("id"), Name: "Ada"})
	}, ezoapi.Route{
		Summary: "Get a user",
		Tags:    []string{"Users"},
		Params: struct {
			ID string `uri:"id" example:"123"`
		}{},
		Response:        reflect.TypeFor[User](),
		ResponseExample: User{ID: "123", Name: "Ada"},
	})
	if err != nil {
		log.Fatal(err)
	}

	app.Get("/openapi.json", api.Handler())
	log.Fatal(app.Listen(":3000"))
}
```

Run the application, then open:

- `http://localhost:3000/api/v1/users/123` — the JSON endpoint.
- `http://localhost:3000/openapi.json` — the generated specification, with the
  documented path `/api/v1/users/{id}`.

Finish registering routes before serving requests, and handle registration
errors at startup. Documentation metadata does not bind or validate live requests.

## From simple endpoints to richer contracts

Start with DTOs and struct tags. Add explicit schemas when your API needs unions,
discriminators, custom JSON representations, or detailed JSON Schema constraints.

| Need | ezoapi support |
| --- | --- |
| Organize endpoints | Nested groups, prefixes, middleware, tags, operation IDs |
| Describe inputs | Path, query, header, cookie, and body metadata |
| Document outcomes | Multiple status codes, response types, and examples |
| Express constraints | Supported `validate` tags, enums, defaults, schema overrides |
| Model advanced payloads | JSON Schema 2020-12, local references, composition, tuples |
| Describe authentication | HTTP and API key security schemes with per-route requirements |
| Export the contract | JSON handler or an independent `Document()` snapshot |

See the [API guide](docs/usage.md) for behavior, examples, and current limits,
or browse the [Go API reference](https://pkg.go.dev/github.com/sonseait/ezoapi).

## Try the included example

```sh
git clone https://github.com/sonseait/ezoapi.git
cd ezoapi
go mod download
go run ./examples/basic
```

The [example application](examples/basic/main.go) listens on
`http://localhost:3000`. Fetch `/openapi.json` for its specification or
`/api/v1/users/18446744073709551615` for its sample response.

For local development and verification commands, see the
[development guide](docs/usage.md#local-development).
