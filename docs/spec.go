// Package docs provides embedded API contract (OpenAPI) for serving and Swagger UI.
package docs

import _ "embed"

//go:embed openapi.yaml
var OpenAPIYAML []byte
