package agents

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type Validator struct {
	schema *jsonschema.Schema
	name   string
}

func NewValidator(projectRoot, schemaFile string) (*Validator, error) {
	path := filepath.Join(projectRoot, "testdata", "contracts", schemaFile)
	reader, err := openSchema(path)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaFile, domain.ErrValidation)
	}
	compiler := jsonschema.NewCompiler()
	// Lock down external $ref resolution: schemas live in testdata/contracts/
	// and must be provided explicitly via AddResource. Reject file://, http://,
	// and any other URI that tries to resolve outside our controlled set.
	compiler.LoadURL = func(s string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("external schema ref rejected: %s", s)
	}
	if err := compiler.AddResource(path, reader); err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaFile, domain.ErrValidation)
	}
	schema, err := compiler.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaFile, domain.ErrValidation)
	}
	return &Validator{schema: schema, name: schemaFile}, nil
}

func (v *Validator) Validate(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("schema %s: marshal: %w", v.name, domain.ErrValidation)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("schema %s: decode: %w", v.name, domain.ErrValidation)
	}
	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("schema %s: %s: %w", v.name, describeValidation(err), domain.ErrValidation)
	}
	return nil
}

func describeValidation(err error) string {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err.Error()
	}
	lines := flattenValidation(ve, nil)
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "; ")
}

func flattenValidation(err *jsonschema.ValidationError, out []string) []string {
	path := err.InstanceLocation
	if path == "" {
		path = "/"
	}
	if len(err.Causes) == 0 {
		return append(out, fmt.Sprintf("%s: %s", path, err.Message))
	}
	for _, cause := range err.Causes {
		out = flattenValidation(cause, out)
	}
	return out
}

func openSchema(path string) (*strings.Reader, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", path, err)
	}
	return strings.NewReader(string(raw)), nil
}
