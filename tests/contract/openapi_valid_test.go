package contract

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestOpenAPISpec_IsValid asserts the document parses and passes OpenAPI 3
// structural validation (info/version present, path params declared, responses
// well-formed). Keeps the gate honest: a spec that satisfies coverage but is
// structurally broken would not codegen.
func TestOpenAPISpec_IsValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load OpenAPI spec %s: %v", specPath, err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("OpenAPI spec %s is invalid: %v", specPath, err)
	}
}
