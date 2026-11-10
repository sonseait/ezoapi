# ezoapi

Đăng ký route Fiber v3 và sinh OpenAPI 3.1 từ DTO bằng reflection.
Module: `github.com/sonseait/ezoapi`. Go 1.27.4, Fiber v3.6.1.

```go
app := fiber.New()
api := ezoapi.New(app, ezoapi.Config{
    Info: ezoapi.Info{Title: "My API", Version: "1.0.0"},
})

type User struct {
    ID      uint64 `json:"id" example:"18446744073709551615"`
    Name    string `json:"name" example:"Ada"`
    Enabled bool   `json:"enabled" example:"false"`
}

err := api.Group("/api/v1").Get("/users/:id", handler, ezoapi.Route{
    Summary: "Get user",
    Params: struct { ID uint64 `uri:"id"` }{},
    Response: reflect.TypeFor[User](),
    ResponseExample: User{ID: 18446744073709551615, Name: "Ada"},
})
if err != nil {
    return err
}
app.Get("/openapi.json", api.Handler())
```

Import `github.com/gofiber/fiber/v3`, `github.com/sonseait/ezoapi` và
`reflect`. Ví dụ hoàn chỉnh: [examples/basic/main.go](examples/basic/main.go).
`/openapi.json` dùng được với công cụ hỗ trợ OpenAPI 3.1 như Swagger UI hoặc Scalar.
Thư viện không nhúng giao diện/CDN.

## Chạy và kiểm tra

```sh
go mod download
gofmt -w .
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
go build ./...
go run ./examples/basic
```

Ví dụ nghe tại `http://localhost:3000`; spec tại `/openapi.json`.
Endpoint mẫu: `/api/v1/users/18446744073709551615`.

Trước khi module được publish, dùng trong project khác qua local replacement:

```sh
go mod edit -replace github.com/sonseait/ezoapi=../ezoapi
go get github.com/sonseait/ezoapi
```

Sau khi publish: `go get github.com/sonseait/ezoapi@<tag>`.

## Route và DTO

- `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Trace`, hoặc
  `Add(method, path, handler, route...)` trả `error`. Luôn xử lý lỗi lúc startup.
- `Group` hỗ trợ prefix lồng nhau và middleware; `Use` chuyển tiếp tới Fiber.
  `New` cũng nhận group đã có. Middleware route chạy trước handler, sau middleware group.
- `Route.Body`, `Query`, `Params`, `Headers`, `Cookies`, `Response` nhận giá trị,
  typed nil pointer hoặc `reflect.TypeFor[T]()`. Giá trị DTO dùng để xác định kiểu;
  sample payload khai báo riêng bằng `BodyExample` / `ResponseExample`.
  `Body` và `Response` cũng nhận `Schema` hoặc `*Schema` để mô tả kiểu nâng cao.
- Parameter dùng tag `query`, `uri`, `header`, `cookie`; fallback sang `json`, rồi
  tên Go field. `"-"` bỏ qua field. Query/header/cookie mặc định không bắt buộc;
  `required:"true"` hoặc token `validate:"required"` đánh dấu bắt buộc.
- `:id` tự chuyển thành `{id}` và path parameter luôn bắt buộc. Nếu không có
  `Params`, kiểu mặc định là string. Wildcard, optional, constraint và parameter
  ghép trong một segment hiện trả lỗi; cần khai báo các route cụ thể.
- Body mặc định bắt buộc; đặt `BodyOptional: true` nếu có thể bỏ qua. Content type
  mặc định là `application/json`. Dùng `BodyContent`, `ResponseContent` hoặc
  `ResponseSpec.Content` cho media type khác hoặc nhiều media type. Parameter hỗ trợ scalar và array scalar; kiểu object
  cần cách serialize riêng chưa được triển khai.
- Response mặc định status 200; đổi bằng `Status`. Không có `Response` nghĩa là
  không khai báo body. Dùng `Responses: map[int]ezoapi.ResponseSpec{...}` để thêm
  các status, DTO và example khác. Không cho body ở HEAD/1xx/204/205/304.
- Route trùng, operation ID trùng, hoặc template chỉ khác tên parameter trả lỗi.
  CONNECT và QUERY chưa biểu diễn được trong OpenAPI 3.1 nên trả lỗi.
- Hoàn tất đăng ký trước khi app phục vụ. Không sửa DTO schema/config/middleware
  metadata sau khi đăng ký. `Document()` trả snapshot độc lập; `Handler()` phục vụ JSON.

## Kiểu dữ liệu và example

Reflect bám theo `encoding/json`: exported fields, rename/ignore, embedded fields,
field dominance, `omitempty`, `omitzero`, `,string`, named types và generic.
Field không có tag `json` vẫn xuất hiện đúng tên Go. Field không có option omit
được đánh dấu required theo hình dạng JSON khi marshal.

Hỗ trợ bool, signed/unsigned integer, float, string, array, slice, map có key
string/integer/TextMarshaler, struct, pointer nhiều cấp, interface và kiểu đệ quy.
Pointer/slice/map cho phép null; array có độ dài cố định. `[]byte` là base64 string,
`[N]byte` là array; `time.Time` là date-time; `json.RawMessage` và interface cho phép
mọi JSON; `json.Number` là number. Các schema dùng `$id` và `$defs` cục bộ để tránh
trùng tên và giữ reference đúng khi nhúng trong OpenAPI.

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

`example`, `default` và `enum` dùng JSON; example/default của string cũng chấp nhận
chuỗi trực tiếp. Enum luôn là JSON array, giữ đúng kiểu từng phần tử. Example
không được đoán từ tên field. Example/default sai kiểu, vượt giới hạn kiểu Go,
không thuộc enum hoặc vi phạm constraint được phát hiện khi đăng ký.
`BodyExample` / `ResponseExample` marshal bằng `encoding/json`, kiểm tra bằng
JSON Schema rồi giữ nguyên JSON, kể cả số lớn, `false`, `0`, `[]`, `{}`, `""`.
Muốn example null tường minh, truyền `json.RawMessage("null")`.

Các rule validator được chuyển: `required`, `min`, `max`, `len`, numeric
`gte/lte/gt/lt`, `oneof` cho string/integer, `unique` cho array, và format
`email`, `uuid`, `uuid4`, `url`/`uri`, `ipv4`, `ipv6`, `hostname`.
`oneof` giữ kiểu số nguyên, kể cả uint64; giá trị chuỗi có khoảng trắng dùng
single quotes như `oneof='red blue' green`. Dừng khi gặp `dive`.
Các rule conditional/custom không suy luận tự động; metadata không thay thế
toàn bộ semantics của validator.
`format` đặt format JSON Schema; format chuẩn và base64 được kiểm tra ở example.
Đây là metadata tài liệu, không thực hiện binding hay runtime validation.

`json.Marshaler` tùy biến cần mapping schema rõ ràng vì reflect không thể biết
mọi kết quả của code marshal. `encoding.TextMarshaler` có value receiver là string.

```go
reflector := ezoapi.Reflector{
    TypeSchemas: map[reflect.Type]*ezoapi.Schema{
        reflect.TypeFor[decimal.Decimal](): {Type: "string", Pattern: `^-?[0-9]+(\.[0-9]+)?$`},
    },
}
api := ezoapi.New(app, ezoapi.Config{Reflector: reflector})
```

Mapping phải đúng cấu hình encoder thực tế của ứng dụng. Function/channel/complex,
map key không tương thích JSON và kiểu không hỗ trợ trả lỗi thay vì đoán object.
Không tải schema tham chiếu bên ngoài. JSON Schema kiểm tra example theo wire
schema; không thay thế việc chạy validator của ứng dụng. Consumer dùng JSON encoder
tùy biến cần cung cấp mapping tương ứng. Bounds integer 64-bit giữ nguyên số JSON,
không chuyển qua float64. `json:",string"` dùng `contentMediaType` và
`contentSchema` để kiểm tra giá trị JSON bên trong string, kể cả bounds integer
và các rule được hỗ trợ. Constraint độ dài byte trước base64 chưa được suy luận
sang độ dài chuỗi base64.

## Schema nâng cao

`Schema` hỗ trợ type union (`Types`), `oneOf`/`anyOf`/`allOf`, discriminator,
`const`, `not`, `if`/`then`/`else`, exclusive bounds, `multipleOf`, tuple
(`prefixItems`), `contains`, `uniqueItems`, pattern/property-name constraints,
dependencies và `unevaluatedProperties`/`unevaluatedItems`.

```go
tuple := &ezoapi.Schema{
    Type: "array",
    PrefixItems: []*ezoapi.Schema{{Type: "string"}, {Type: "integer"}},
    Items: ezoapi.BoolSchema(false), // không có phần tử thứ ba
}
nullableString := &ezoapi.Schema{Types: []string{"string", "null"}}
closedObject := &ezoapi.Schema{
    Type: "object",
    Properties: map[string]*ezoapi.Schema{"name": {Type: "string"}},
    AdditionalProperties: ezoapi.BoolSchema(false),
}
// Route{Body: closedObject, Response: tuple}
```

`BoolSchema(true)` chấp nhận mọi giá trị, `BoolSchema(false)` không chấp nhận
giá trị nào. `Type` và `Types` không được dùng đồng thời. `Const` và `Default`
là `json.RawMessage` để biểu diễn rõ `null`, `false`, `0`; ví dụ
`Const: json.RawMessage("null")`. Numeric bounds và `MultipleOf` nhận số Go hoặc
`json.Number` để giữ chính xác số lớn/số thập phân.

Union không thể suy ra từ interface. Khai báo các variant trong `Defs`, tham chiếu
bằng `OneOf`, rồi đặt `Discriminator.PropertyName` và `Mapping`, ví dụ
`"cat": "#/$defs/Cat"`. Mỗi variant dùng `const`/`enum` và `required` để ràng buộc
field phân loại; discriminator là metadata cho tooling, không tự tạo ràng buộc.
Mapping phải dùng reference tường minh đến schema cục bộ và được kiểm tra khi
đăng ký. Xem ví dụ hoàn chỉnh trong [advanced_test.go](advanced_test.go).

Override từng field bằng JSON Schema trong tag `schema`:

```go
type Event struct {
    Value any `json:"value" schema:"{\"type\":[\"string\",\"integer\"]}" example:"0"`
    Secret string `json:"secret" schema:"{\"type\":\"string\",\"writeOnly\":true}"`
}
```

Các keyword chưa có field Go dùng `Schema.Extra`, chẳng hạn
`Extra: map[string]any{"x-unit": "USD"}`. Keyword lạ được giữ nguyên qua
marshal/unmarshal và snapshot; Extra không được ghi đè keyword đã có field Go.
Schema được clone trước khi dùng, kiểm tra cấu trúc cùng example/default trong
mọi nhánh và `$defs`. Dùng `$ref` để biểu diễn đệ quy.

## Form, file và các content type khác

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
        "text/plain": {Schema: &ezoapi.Schema{Type: "string"}, Example: "ok"},
        "application/octet-stream": {}, // raw binary, không áp JSON encoding
    },
}
```

Cùng API dùng cho `application/x-www-form-urlencoded`, `text/event-stream`,
media type riêng hoặc JSON có schema override. `Encoding` chỉ áp dụng cho form
và multipart. `BodyContent` không dùng đồng thời với `Body`/`BodyExample`;
`ResponseContent` hoặc `ResponseSpec.Content` không dùng đồng thời với DTO/example
response tương ứng. Handler tự bind form/file và viết response/stream bằng Fiber.
Content metadata không thay đổi runtime handler; `[]byte` trong JSON vẫn là base64.

## Chuyển từ PGW

Chuyển `BodyDTO/QueryDTO/ResponseDTO` thành `Body/Query/Response`, `Middlewares`
thành `Middleware`. Response envelope là kiểu của ứng dụng, ví dụ:

```go
type Envelope[T any] struct {
    Code string `json:"code" example:"ok"`
    RequestID string `json:"requestId"`
    Data T `json:"data"`
}
// Response: reflect.TypeFor[Envelope[PaginateResult[User]]]()
```

Generic được reflect trực tiếp nên không cần `ItemDTO`. Auth khai báo qua
`Config.SecuritySchemes` (http/apiKey) và `Route.Security`, ví dụ
`[]map[string][]string{{"bearerAuth": {}}}`; cài middleware xác thực riêng.
Thư viện không gắn cứng envelope, JWT, pagination hay các tích hợp vận hành của PGW.

