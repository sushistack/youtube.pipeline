package eval

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

const (
	fixtureSchemaFile = "golden_eval_fixture.schema.json"
	inputSchemaFile   = "writer_output.schema.json"

	kindPositive = "positive"
	kindNegative = "negative"

	verdictPass  = "pass"
	verdictRetry = "retry"

	checkpointPostWriter = "post_writer"
)

// ValidateFixture parses raw JSON, validates the outer envelope against
// golden_eval_fixture.schema.json, validates the nested input against
// writer_output.schema.json, and enforces kind/verdict consistency.
// Returns the parsed Fixture or a domain.ErrValidation-wrapped error.
func ValidateFixture(projectRoot string, raw []byte) (Fixture, error) {
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return Fixture{}, fmt.Errorf("malformed JSON: %w", domain.ErrValidation)
	}

	if err := validateAgainstSchema(projectRoot, fixtureSchemaFile, raw); err != nil {
		return Fixture{}, err
	}

	if err := validateAgainstSchema(projectRoot, inputSchemaFile, []byte(f.Input)); err != nil {
		return Fixture{}, fmt.Errorf("input field: %w", err)
	}

	if err := validateKindVerdict(f); err != nil {
		return Fixture{}, err
	}

	if f.Checkpoint != checkpointPostWriter {
		return Fixture{}, fmt.Errorf("unsupported checkpoint %q (only %q allowed in Story 4.1): %w",
			f.Checkpoint, checkpointPostWriter, domain.ErrValidation)
	}

	return f, nil
}

func validateKindVerdict(f Fixture) error {
	switch f.Kind {
	case kindPositive:
		if f.ExpectedVerdict != verdictPass {
			return fmt.Errorf("positive fixture must have expected_verdict=%q, got %q: %w",
				verdictPass, f.ExpectedVerdict, domain.ErrValidation)
		}
	case kindNegative:
		if f.ExpectedVerdict != verdictRetry {
			return fmt.Errorf("negative fixture must have expected_verdict=%q, got %q: %w",
				verdictRetry, f.ExpectedVerdict, domain.ErrValidation)
		}
	default:
		return fmt.Errorf("unknown kind %q (must be %q or %q): %w",
			f.Kind, kindPositive, kindNegative, domain.ErrValidation)
	}
	return nil
}

func validateAgainstSchema(projectRoot, schemaFile string, data []byte) error {
	schemaPath := filepath.Join(projectRoot, "testdata", "contracts", schemaFile)
	sch, err := compileSchema(schemaPath)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("decode for schema %s: %w", schemaFile, domain.ErrValidation)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("schema %s: %s: %w", schemaFile, describeSchemaError(err), domain.ErrValidation)
	}
	return nil
}

func compileSchema(schemaPath string) (*jsonschema.Schema, error) {
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", schemaPath, domain.ErrValidation)
	}
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(s string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("external schema ref rejected: %s", s)
	}
	if err := compiler.AddResource(schemaPath, strings.NewReader(string(raw))); err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaPath, domain.ErrValidation)
	}
	sch, err := compiler.Compile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", schemaPath, domain.ErrValidation)
	}
	return sch, nil
}

func describeSchemaError(err error) string {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err.Error()
	}
	lines := flattenSchemaErrors(ve, nil)
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "; ")
}

func flattenSchemaErrors(err *jsonschema.ValidationError, out []string) []string {
	loc := err.InstanceLocation
	if loc == "" {
		loc = "/"
	}
	if len(err.Causes) == 0 {
		return append(out, fmt.Sprintf("%s: %s", loc, err.Message))
	}
	for _, cause := range err.Causes {
		out = flattenSchemaErrors(cause, out)
	}
	return out
}
