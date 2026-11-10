package ezoapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
)

type Config struct {
	Info            Info
	Servers         []Server
	Reflector       Reflector
	SecuritySchemes map[string]SecurityScheme
}

// Route describes the DTOs used by a handler. A DTO may be a value, a typed nil
// pointer, or reflect.TypeFor[T](). It does not execute or inspect the handler.
type Route struct {
	Summary, Description, OperationID     string
	Tags                                  []string
	Deprecated                            bool
	Body, Query, Params, Headers, Cookies any
	BodyOptional                          bool
	BodyExample                           any
	BodyContent                           map[string]MediaType
	Response                              any
	ResponseExample                       any
	ResponseContent                       map[string]MediaType
	Status                                int // Defaults to 200.
	Responses                             map[int]ResponseSpec
	Middleware                            []fiber.Handler
	Security                              []map[string][]string // Describes auth; middleware enforces it.
}

type ResponseSpec struct {
	Description string
	DTO         any
	Example     any
	Content     map[string]MediaType
}

type registry struct {
	sync.RWMutex
	doc       Document
	reflector Reflector
}

type Router struct {
	router   fiber.Router
	prefix   string
	registry *registry
}

// New wraps an app or an existing Fiber group. Registration must finish before
// serving requests, as with Fiber itself. Document and Handler are safe for reads.
func New(router fiber.Router, config Config) *Router {
	prefix := ""
	if g, ok := router.(*fiber.Group); ok {
		prefix = g.Prefix
	}
	if config.Info.Title == "" {
		config.Info.Title = "API"
	}
	if config.Info.Version == "" {
		config.Info.Version = "1.0.0"
	}
	var components *Components
	if len(config.SecuritySchemes) > 0 {
		components = &Components{SecuritySchemes: config.SecuritySchemes}
	}
	return &Router{router: router, prefix: prefix, registry: &registry{
		doc:       Document{OpenAPI: "3.1.0", Info: config.Info, Servers: config.Servers, Paths: map[string]map[string]*Operation{}, Components: components},
		reflector: config.Reflector,
	}}
}

func (r *Router) Group(prefix string, middleware ...fiber.Handler) *Router {
	handlers := make([]any, len(middleware))
	for i, h := range middleware {
		handlers[i] = h
	}
	return &Router{router: r.router.Group(prefix, handlers...), prefix: joinPath(r.prefix, prefix), registry: r.registry}
}

// Use delegates middleware registration to Fiber. It does not add a documented operation.
func (r *Router) Use(args ...any) { r.router.Use(args...) }

func (r *Router) Get(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodGet, path, handler, route...)
}
func (r *Router) Post(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodPost, path, handler, route...)
}
func (r *Router) Put(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodPut, path, handler, route...)
}
func (r *Router) Patch(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodPatch, path, handler, route...)
}
func (r *Router) Delete(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodDelete, path, handler, route...)
}
func (r *Router) Head(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodHead, path, handler, route...)
}
func (r *Router) Options(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodOptions, path, handler, route...)
}
func (r *Router) Trace(path string, handler fiber.Handler, route ...Route) error {
	return r.Add(http.MethodTrace, path, handler, route...)
}

// Add validates all documentation before registering the route. Unsupported or
// ambiguous Fiber path syntax returns an error instead of producing misleading docs.
func (r *Router) Add(method, path string, handler fiber.Handler, routes ...Route) error {
	method = strings.ToLower(method)
	if !slices.Contains([]string{"get", "post", "put", "patch", "delete", "head", "options", "trace"}, method) {
		return fmt.Errorf("ezoapi: method %q is not supported by OpenAPI 3.1", method)
	}
	if handler == nil {
		return fmt.Errorf("ezoapi: nil handler")
	}
	if len(routes) > 1 {
		return fmt.Errorf("ezoapi: expected at most one Route")
	}
	var cfg Route
	if len(routes) == 1 {
		cfg = routes[0]
	}
	fullPath, names, err := openAPIPath(joinPath(r.prefix, path))
	if err != nil {
		return err
	}
	op := &Operation{Summary: cfg.Summary, Description: cfg.Description, Tags: cfg.Tags, OperationID: cfg.OperationID, Deprecated: cfg.Deprecated, Responses: map[string]Response{}}
	op.Security = cfg.Security
	for _, requirement := range cfg.Security {
		for name, scopes := range requirement {
			components := r.registry.doc.Components
			if components == nil {
				return fmt.Errorf("ezoapi: unknown security scheme %q", name)
			}
			scheme, ok := components.SecuritySchemes[name]
			if !ok {
				return fmt.Errorf("ezoapi: unknown security scheme %q", name)
			}
			if scheme.Type != "http" && scheme.Type != "apiKey" {
				return fmt.Errorf("ezoapi: unsupported security scheme %q", scheme.Type)
			}
			if scheme.Type == "http" && scheme.Scheme == "" {
				return fmt.Errorf("ezoapi: HTTP security scheme needs scheme")
			}
			if scheme.Type == "apiKey" && (scheme.Name == "" || !slices.Contains([]string{"header", "query", "cookie"}, scheme.In)) {
				return fmt.Errorf("ezoapi: invalid apiKey security scheme")
			}
			if len(scopes) > 0 || scopes == nil {
				return fmt.Errorf("ezoapi: http/apiKey security scopes must be []string{}")
			}
		}
	}
	if op.OperationID == "" {
		op.OperationID = method + " " + fullPath
	}
	for _, location := range []struct {
		in  string
		dto any
	}{{"path", cfg.Params}, {"query", cfg.Query}, {"header", cfg.Headers}, {"cookie", cfg.Cookies}} {
		params, err := r.parameters(location.dto, location.in)
		if err != nil {
			return err
		}
		op.Parameters = append(op.Parameters, params...)
	}
	for _, p := range op.Parameters {
		if p.In == "path" && !slices.Contains(names, p.Name) {
			return fmt.Errorf("ezoapi: path parameter %q is absent from %s", p.Name, fullPath)
		}
	}
	for _, name := range names {
		if !slices.ContainsFunc(op.Parameters, func(p Parameter) bool { return p.In == "path" && p.Name == name }) {
			op.Parameters = append(op.Parameters, Parameter{Name: name, In: "path", Required: true, Schema: &Schema{Type: "string"}})
		}
	}
	if cfg.BodyContent != nil {
		if cfg.Body != nil || cfg.BodyExample != nil {
			return fmt.Errorf("ezoapi: use BodyContent or Body/BodyExample")
		}
		content, err := r.content(cfg.BodyContent)
		if err != nil {
			return err
		}
		op.RequestBody = &RequestBody{Required: !cfg.BodyOptional, Content: content}
	} else if cfg.Body != nil {
		media, err := r.media(cfg.Body, cfg.BodyExample)
		if err != nil {
			return err
		}
		op.RequestBody = &RequestBody{Required: !cfg.BodyOptional, Content: map[string]MediaType{"application/json": media}}
	} else if cfg.BodyExample != nil {
		return fmt.Errorf("ezoapi: BodyExample needs Body")
	}
	status := cfg.Status
	if status == 0 {
		status = http.StatusOK
	}
	responses := make(map[int]ResponseSpec, len(cfg.Responses)+1)
	for code, spec := range cfg.Responses {
		responses[code] = spec
	}
	if _, exists := responses[status]; !exists {
		responses[status] = ResponseSpec{DTO: cfg.Response, Example: cfg.ResponseExample, Content: cfg.ResponseContent}
	} else if cfg.Response != nil || cfg.ResponseExample != nil || cfg.ResponseContent != nil {
		return fmt.Errorf("ezoapi: response %d specified twice", status)
	}
	for code, spec := range responses {
		if code < 100 || code > 599 {
			return fmt.Errorf("ezoapi: invalid response status %d", code)
		}
		response := Response{Description: spec.Description}
		if response.Description == "" {
			response.Description = http.StatusText(code)
			if response.Description == "" {
				response.Description = "Response"
			}
		}
		if spec.DTO != nil || spec.Content != nil {
			if code < 200 || code == 204 || code == 205 || code == 304 || method == "head" {
				return fmt.Errorf("ezoapi: %s response %d cannot have a body", method, code)
			}
			if spec.Content != nil {
				if spec.DTO != nil || spec.Example != nil {
					return fmt.Errorf("ezoapi: use response Content or DTO/Example")
				}
				content, err := r.content(spec.Content)
				if err != nil {
					return err
				}
				response.Content = content
			} else {
				media, err := r.media(spec.DTO, spec.Example)
				if err != nil {
					return err
				}
				response.Content = map[string]MediaType{"application/json": media}
			}
		} else if spec.Example != nil {
			return fmt.Errorf("ezoapi: response example needs DTO")
		}
		op.Responses[strconv.Itoa(code)] = response
	}
	reg := r.registry
	reg.Lock()
	defer reg.Unlock()
	for existingPath, item := range reg.doc.Paths {
		if pathShape(existingPath) == pathShape(fullPath) && existingPath != fullPath {
			return fmt.Errorf("ezoapi: ambiguous path templates %s and %s", existingPath, fullPath)
		}
		for existingMethod, existing := range item {
			if existing.OperationID == op.OperationID {
				return fmt.Errorf("ezoapi: duplicate operationId %q", op.OperationID)
			}
			if existingPath == fullPath && existingMethod == method {
				return fmt.Errorf("ezoapi: duplicate route %s %s", method, fullPath)
			}
		}
	}
	for _, mw := range cfg.Middleware {
		if mw == nil {
			return fmt.Errorf("ezoapi: nil middleware")
		}
	}
	handlers := make([]any, 0, len(cfg.Middleware)+1)
	for _, mw := range cfg.Middleware {
		handlers = append(handlers, mw)
	}
	handlers = append(handlers, handler)
	r.router.Add([]string{strings.ToUpper(method)}, path, handlers[0], handlers[1:]...)
	if reg.doc.Paths[fullPath] == nil {
		reg.doc.Paths[fullPath] = map[string]*Operation{}
	}
	reg.doc.Paths[fullPath][method] = op
	return nil
}

func (r *Router) content(input map[string]MediaType) (map[string]MediaType, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("ezoapi: content must have at least one media type")
	}
	// A JSON round trip freezes caller metadata and preserves false/zero examples.
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var content map[string]MediaType
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&content); err != nil {
		return nil, err
	}
	for name, media := range content {
		kind, _, err := mime.ParseMediaType(name)
		if err != nil {
			return nil, fmt.Errorf("ezoapi: invalid media type %q: %w", name, err)
		}
		major, minor, ok := strings.Cut(kind, "/")
		if !ok || major == "" || minor == "" {
			return nil, fmt.Errorf("ezoapi: invalid media type %q", name)
		}
		if media.Schema != nil {
			schema, err := prepareSchema(media.Schema)
			if err != nil {
				return nil, err
			}
			media.Schema = schema
		}
		if media.Example != nil {
			schema := media.Schema
			if schema == nil {
				schema = &Schema{}
			}
			example, err := ValidateExample(schema, media.Example)
			if err != nil {
				return nil, err
			}
			media.Example = example
		}
		if len(media.Encoding) > 0 && kind != "multipart/form-data" && kind != "application/x-www-form-urlencoded" {
			return nil, fmt.Errorf("ezoapi: encoding is only supported for multipart/form-data or application/x-www-form-urlencoded")
		}
		for field, encoding := range media.Encoding {
			if field == "" {
				return nil, fmt.Errorf("ezoapi: empty encoding field")
			}
			if encoding.ContentType != "" {
				if _, _, err := mime.ParseMediaType(encoding.ContentType); err != nil {
					return nil, fmt.Errorf("ezoapi: invalid encoding content type %q", encoding.ContentType)
				}
			}
			if encoding.Style != "" && !slices.Contains([]string{"form", "spaceDelimited", "pipeDelimited", "deepObject"}, encoding.Style) {
				return nil, fmt.Errorf("ezoapi: invalid encoding style %q", encoding.Style)
			}
		}
		content[name] = media
	}
	return content, nil
}

func (r *Router) media(dto, example any) (MediaType, error) {
	s, err := r.registry.reflector.Schema(dto)
	if err != nil {
		return MediaType{}, err
	}
	media := MediaType{Schema: s}
	if example != nil {
		data, err := ValidateExample(s, example)
		if err != nil {
			return media, err
		}
		media.Example = data
	}
	return media, nil
}

func (r *Router) parameters(dto any, in string) ([]Parameter, error) {
	if dto == nil {
		return nil, nil
	}
	t, ok := dto.(reflect.Type)
	if !ok {
		t = reflect.TypeOf(dto)
	}
	t = indirectType(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("ezoapi: %s DTO must be a struct", in)
	}
	tag := in
	if in == "path" {
		tag = "uri"
	}
	b := schemaBuilder{reflector: r.registry.reflector, defs: map[string]*Schema{}}
	var params []Parameter
	for _, f := range jsonFields(t, tag) {
		s, err := b.field(f)
		if err != nil {
			return nil, fmt.Errorf("%s parameter %s: %w", in, f.name, err)
		}
		// ponytail: parameters accept scalar/array DTOs; add explicit style
		// configuration when deepObject or custom parameter serialization is needed.
		if !parameterSchema(s, b.defs, true, 0) {
			return nil, fmt.Errorf("ezoapi: %s parameter %s needs a scalar/array wire type", in, f.name)
		}
		if in == "header" {
			if slices.Contains([]string{"accept", "content-type", "authorization"}, strings.ToLower(f.name)) {
				return nil, fmt.Errorf("ezoapi: OpenAPI ignores reserved header parameter %q", f.name)
			}
			if slices.ContainsFunc(params, func(p Parameter) bool { return strings.EqualFold(p.Name, f.name) }) {
				return nil, fmt.Errorf("ezoapi: duplicate header parameter %q", f.name)
			}
		}
		s.Defs = b.defs
		if err := validateSchema(s); err != nil {
			return nil, err
		}
		if err := identify(s); err != nil {
			return nil, err
		}
		required := in == "path" || f.field.Tag.Get("required") == "true" || slices.Contains(strings.Split(f.field.Tag.Get("validate"), ","), "required")
		params = append(params, Parameter{Name: f.name, In: in, Required: required, Description: s.Description, Schema: s})
	}
	return params, nil
}

func parameterSchema(s *Schema, defs map[string]*Schema, array bool, depth int) bool {
	if s == nil || depth > 32 {
		return false
	}
	if len(s.Defs) > 0 {
		defs = s.Defs
	}
	if s.Ref != "" {
		return parameterSchema(defs[strings.TrimPrefix(s.Ref, "#/$defs/")], defs, array, depth+1)
	}
	if len(s.AnyOf) > 0 {
		for _, branch := range s.AnyOf {
			if !parameterSchema(branch, defs, array, depth+1) {
				return false
			}
		}
		return true
	}
	if s.Types != nil {
		if len(s.Types) == 0 {
			return false
		}
		for _, kind := range s.Types {
			copy := *s
			copy.Type = kind
			copy.Types = nil
			if !parameterSchema(&copy, defs, array, depth+1) {
				return false
			}
		}
		return true
	}
	if slices.Contains([]string{"string", "integer", "number", "boolean", "null"}, s.Type) {
		return true
	}
	return array && s.Type == "array" && parameterSchema(s.Items, defs, false, depth+1)
}

// Document returns an independent snapshot; callers may safely modify it.
func (r *Router) Document() (*Document, error) {
	r.registry.RLock()
	defer r.registry.RUnlock()
	data, err := json.Marshal(r.registry.doc)
	if err != nil {
		return nil, err
	}
	var doc Document
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Handler serves the current OpenAPI document. Register it on the underlying app.
func (r *Router) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		r.registry.RLock()
		data, err := json.Marshal(r.registry.doc)
		r.registry.RUnlock()
		if err != nil {
			return err
		}
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		return c.Send(data)
	}
}

func joinPath(prefix, path string) string {
	if path == "" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if path[0] != '/' {
		path = "/" + path
	}
	return strings.TrimRight(prefix, "/") + path
}

func openAPIPath(path string) (string, []string, error) {
	parts := strings.Split(path, "/")
	var names []string
	for i, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ":") {
			name := part[1:]
			for _, c := range name {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
					return "", nil, fmt.Errorf("ezoapi: unsupported Fiber path segment %q (optional, wildcard, constrained and mixed parameters need explicit expansion)", part)
				}
			}
			if name == "" || slices.Contains(names, name) {
				return "", nil, fmt.Errorf("ezoapi: empty or duplicate path parameter %q", name)
			}
			names = append(names, name)
			parts[i] = "{" + name + "}"
		} else if strings.ContainsAny(part, ":*+?{}\\<>") {
			return "", nil, fmt.Errorf("ezoapi: unsupported Fiber path segment %q", part)
		}
	}
	return strings.Join(parts, "/"), names, nil
}

func pathShape(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") {
			parts[i] = "{}"
		}
	}
	return strings.Join(parts, "/")
}
