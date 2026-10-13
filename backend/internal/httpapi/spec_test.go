package httpapi_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	spec "github.com/whoaskedssselery/studyvault/backend/api"
)

// Swagger UI и генераторы клиентов читают этот файл, поэтому он обязан быть настоящим OpenAPI-документом:
// опечатка в таблице внутри описания ломает разбор целиком, а тест на префикс файла этого не видит.
func TestSpec_IsAValidOpenAPIDocument(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(spec.OpenAPI)
	if err != nil {
		t.Fatalf("the contract does not parse: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("the contract is not valid OpenAPI: %v", err)
	}
	// Строка вроде «3.0.3 | что-то» тоже разбирается, но Swagger UI отвергает такую версию, поэтому проверяем точно.
	if !regexp.MustCompile(`^3\.0\.\d+$`).MatchString(doc.OpenAPI) {
		t.Errorf("openapi version = %q, want 3.0.x exactly", doc.OpenAPI)
	}
	if doc.Info.Version == "" || doc.Info.License == nil {
		t.Errorf("info = %+v: version and license are required", doc.Info)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "/v1" {
		t.Errorf("servers = %+v, want exactly /v1", doc.Servers)
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" || len(op.Responses.Map()) == 0 {
				t.Errorf("%s %s: no operationId or no responses", method, path)
			}
		}
	}
}
