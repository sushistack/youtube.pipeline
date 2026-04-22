package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type CorpusReader interface {
	Read(ctx context.Context, scpID string) (CorpusDocument, error)
}

type CorpusDocument struct {
	SCPID    string
	Facts    SCPFacts
	Meta     SCPMeta
	MainText string
}

type SCPFacts struct {
	SCPID                 string            `json:"scp_id"`
	Title                 string            `json:"title"`
	ObjectClass           string            `json:"object_class"`
	Rating                int               `json:"rating"`
	PhysicalDescription   string            `json:"physical_description"`
	AnomalousProperties   []string          `json:"anomalous_properties"`
	ContainmentProcedures string            `json:"containment_procedures"`
	BehaviorAndNature     string            `json:"behavior_and_nature"`
	OriginAndDiscovery    string            `json:"origin_and_discovery"`
	Incidents             []any             `json:"incidents"`
	RelatedDocuments      []any             `json:"related_documents"`
	VisualElements        SCPVisualElements `json:"visual_elements"`
	CrossReferences       []any             `json:"cross_references"`
	Tags                  []string          `json:"tags"`
}

type SCPVisualElements struct {
	Appearance             string   `json:"appearance"`
	DistinguishingFeatures []string `json:"distinguishing_features"`
	EnvironmentSetting     string   `json:"environment_setting"`
	KeyVisualMoments       []string `json:"key_visual_moments"`
}

type SCPMeta struct {
	SCPID       string   `json:"scp_id"`
	Tags        []string `json:"tags"`
	RelatedDocs []string `json:"related_docs"`
	AuthorName  string   `json:"author_name"`
	SourceURL   string   `json:"source_url"`
}

var ErrCorpusNotFound = fmt.Errorf("corpus not found: %w", domain.ErrNotFound)

type filesystemCorpus struct {
	dataDir string
}

func NewFilesystemCorpus(dataDir string) CorpusReader {
	return filesystemCorpus{dataDir: dataDir}
}

func (c filesystemCorpus) Read(ctx context.Context, scpID string) (CorpusDocument, error) {
	if err := ctx.Err(); err != nil {
		return CorpusDocument{}, err
	}

	// Reject scpIDs that would escape the corpus root: empty, path separators,
	// parent traversal, or absolute paths. Treat as missing (404) rather than
	// leaking filesystem structure via a different error class.
	if scpID == "" || strings.ContainsAny(scpID, `/\`) || strings.Contains(scpID, "..") || filepath.IsAbs(scpID) {
		return CorpusDocument{}, ErrCorpusNotFound
	}

	dir := filepath.Join(c.dataDir, scpID)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return CorpusDocument{}, ErrCorpusNotFound
		}
		return CorpusDocument{}, fmt.Errorf("stat corpus dir %s: %w", dir, domain.ErrStageFailed)
	}
	if !info.IsDir() {
		return CorpusDocument{}, ErrCorpusNotFound
	}

	// Ensure the resolved corpus directory stays inside dataDir after symlink
	// resolution — a symlinked corpus directory must not redirect reads to
	// arbitrary filesystem paths.
	if err := ensureWithinRoot(c.dataDir, dir); err != nil {
		return CorpusDocument{}, err
	}

	facts, err := readJSONFile[SCPFacts](filepath.Join(dir, "facts.json"))
	if err != nil {
		return CorpusDocument{}, err
	}
	meta, err := readJSONFile[SCPMeta](filepath.Join(dir, "meta.json"))
	if err != nil {
		return CorpusDocument{}, err
	}
	mainPath := filepath.Join(dir, "main.txt")
	mainInfo, err := os.Stat(mainPath)
	if err != nil {
		if os.IsNotExist(err) {
			return CorpusDocument{}, fmt.Errorf("read main.txt: %w", domain.ErrValidation)
		}
		return CorpusDocument{}, fmt.Errorf("read main.txt: %w", domain.ErrStageFailed)
	}
	if !mainInfo.Mode().IsRegular() {
		return CorpusDocument{}, fmt.Errorf("read main.txt: not a regular file: %w", domain.ErrValidation)
	}
	mainRaw, err := os.ReadFile(mainPath)
	if err != nil {
		return CorpusDocument{}, fmt.Errorf("read main.txt: %w", domain.ErrStageFailed)
	}
	if !utf8.Valid(mainRaw) {
		return CorpusDocument{}, fmt.Errorf("read main.txt: invalid UTF-8: %w", domain.ErrValidation)
	}

	return CorpusDocument{
		SCPID:    scpID,
		Facts:    facts,
		Meta:     meta,
		MainText: string(mainRaw),
	}, nil
}

// ensureWithinRoot rejects a resolved path that escapes the corpus root via
// symlink. Surfaces as ErrCorpusNotFound to avoid leaking layout via a distinct
// error class.
func ensureWithinRoot(root, dir string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// Root missing or unreadable — caller will surface a clearer stat error on dir itself.
		resolvedRoot = root
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve corpus dir %s: %w", dir, domain.ErrStageFailed)
	}
	absRoot, err := filepath.Abs(resolvedRoot)
	if err != nil {
		return fmt.Errorf("abs root %s: %w", resolvedRoot, domain.ErrStageFailed)
	}
	absDir, err := filepath.Abs(resolvedDir)
	if err != nil {
		return fmt.Errorf("abs dir %s: %w", resolvedDir, domain.ErrStageFailed)
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrCorpusNotFound
	}
	return nil
}

func readJSONFile[T any](path string) (T, error) {
	var out T
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, fmt.Errorf("read %s: %w", filepath.Base(path), domain.ErrValidation)
		}
		return out, fmt.Errorf("read %s: %w", filepath.Base(path), domain.ErrStageFailed)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse %s: %w", filepath.Base(path), domain.ErrValidation)
	}
	return out, nil
}
