// Package api содержит OpenAPI-контракт, встроенный в бинарник, чтобы сервер мог отдавать его сам.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
