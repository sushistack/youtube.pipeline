package pipeline

import (
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func ValidateDistinctProviders(writerProvider, criticProvider string) error {
	if writerProvider == "" || criticProvider == "" {
		return fmt.Errorf("%w: writer and critic providers must be non-empty", domain.ErrValidation)
	}
	if writerProvider == criticProvider {
		// Wrap domain.ErrValidation so errors.Is + domain.Classify continue to
		// work, while preserving the exact substring "Writer and Critic must
		// use different LLM providers" in err.Error() — this phrase is a
		// contract pinned by the spec (see AC-WRITER-CRITIC) and by
		// internal/config/doctor.go's matching on the doctor CLI output.
		return fmt.Errorf("%w: Writer and Critic must use different LLM providers", domain.ErrValidation)
	}
	return nil
}
