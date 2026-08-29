package recap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

var safeArtifactID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	artifactKindCount = 3
	artifactDirMode   = 0o755
	artifactFileMode  = 0o600
)

// Artifact is one completed local-memory file and its content digest. Path is
// relative to the project workdir so it remains portable across machines.
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ArtifactManifest identifies the recap file and optional generated lists.
type ArtifactManifest struct {
	Sessions      Artifact  `json:"sessions"`
	Questions     *Artifact `json:"questions,omitempty"`
	ThingsToAvoid *Artifact `json:"things_to_avoid,omitempty"`
}

// DBArtifacts converts a completed local manifest to the durable DB shape.
// Empty optional artifacts remain zero-valued and are accepted by the ledger.
func (m ArtifactManifest) DBArtifacts() db.RecapArtifacts {
	out := db.RecapArtifacts{
		Sessions: db.RecapArtifact{Path: m.Sessions.Path, SHA256: m.Sessions.SHA256},
	}
	if m.Questions != nil {
		out.Questions = db.RecapArtifact{Path: m.Questions.Path, SHA256: m.Questions.SHA256}
	}
	if m.ThingsToAvoid != nil {
		out.ThingsToAvoid = db.RecapArtifact{Path: m.ThingsToAvoid.Path, SHA256: m.ThingsToAvoid.SHA256}
	}
	return out
}

// MaterializeInput contains one already-generated recap envelope and its
// immutable source snapshot.
type MaterializeInput struct {
	RecapID     string
	Model       string
	GeneratedAt time.Time
	Snapshot    Snapshot
	Envelope    Envelope
}

// Materialize writes all artifacts under the project workdir. Every file is
// synced and renamed from a temporary sibling; a later failure cleans up files
// from this invocation so callers never receive a partial manifest.
func Materialize(ctx context.Context, workdir string, input MaterializeInput) (ArtifactManifest, error) {
	if err := requireContext(ctx); err != nil {
		return ArtifactManifest{}, err
	}
	root, err := filepath.Abs(workdir)
	if err != nil {
		return ArtifactManifest{}, fmt.Errorf("recap: resolve workdir: %w", err)
	}
	root = filepath.Clean(root)
	if err := validateMaterializeInput(input); err != nil {
		return ArtifactManifest{}, err
	}
	generatedAt := input.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	generatedAt = generatedAt.UTC()
	stem := fmt.Sprintf("%012d-%s.md", input.Snapshot.SourceEndSeq, input.Snapshot.SourceEndMessageID)
	base := filepath.Join("knowledge-base", "recaps")
	directories := map[string]string{
		"sessions":        filepath.Join(base, "sessions", input.Snapshot.SessionID),
		"questions":       filepath.Join(base, "questions", input.Snapshot.SessionID),
		"things-to-avoid": filepath.Join(base, "things-to-avoid", input.Snapshot.SessionID),
	}
	paths := make([]string, 0, artifactKindCount)
	for _, relativeDir := range directories {
		if _, err := safeJoin(root, relativeDir, stem); err != nil {
			return ArtifactManifest{}, err
		}
	}
	manifest := ArtifactManifest{}
	write := func(kind, relativeDir, body string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath := filepath.Join(relativeDir, stem)
		absolutePath, err := safeJoin(root, relativeDir, stem)
		if err != nil {
			return err
		}
		digest, err := writeAtomic(ctx, absolutePath, []byte(body))
		if err != nil {
			return err
		}
		paths = append(paths, absolutePath)
		artifact := Artifact{Path: filepath.ToSlash(relativePath), SHA256: digest}
		switch kind {
		case "sessions":
			manifest.Sessions = artifact
		case "questions":
			manifest.Questions = &artifact
		case "things-to-avoid":
			manifest.ThingsToAvoid = &artifact
		}
		return nil
	}

	header := frontMatter(input, generatedAt)
	if err := write("sessions", directories["sessions"], header+input.Envelope.RecapMarkdown+"\n"); err != nil {
		cleanupArtifacts(paths)
		return ArtifactManifest{}, fmt.Errorf("recap: write sessions artifact: %w", err)
	}
	if len(input.Envelope.Questions) > 0 {
		if err := write("questions", directories["questions"], header+questionsMarkdown(input.Envelope.Questions)); err != nil {
			cleanupArtifacts(paths)
			return ArtifactManifest{}, fmt.Errorf("recap: write questions artifact: %w", err)
		}
	}
	if len(input.Envelope.ThingsToAvoid) > 0 {
		if err := write("things-to-avoid", directories["things-to-avoid"], header+avoidMarkdown(input.Envelope.ThingsToAvoid)); err != nil {
			cleanupArtifacts(paths)
			return ArtifactManifest{}, fmt.Errorf("recap: write things-to-avoid artifact: %w", err)
		}
	}
	return manifest, nil
}

func validateMaterializeInput(input MaterializeInput) error {
	if strings.TrimSpace(input.RecapID) == "" || !safeArtifactID.MatchString(input.RecapID) {
		return errors.New("recap: invalid recap ID")
	}
	if strings.TrimSpace(input.Model) == "" || strings.ContainsAny(input.Model, "\r\n") {
		return errors.New("recap: invalid model")
	}
	if !safeArtifactID.MatchString(input.Snapshot.SessionID) {
		return errors.New("recap: invalid session ID")
	}
	if !safeArtifactID.MatchString(input.Snapshot.SourceEndMessageID) {
		return errors.New("recap: invalid source end message ID")
	}
	if input.Snapshot.SourceStartSeq <= 0 || input.Snapshot.SourceEndSeq <= 0 || input.Snapshot.SourceStartSeq > input.Snapshot.SourceEndSeq {
		return errors.New("recap: invalid snapshot source range")
	}
	if len(input.Snapshot.Messages) < minimumMessageCount {
		return ErrInsufficientMessages
	}
	if err := validateEnvelope(input.Envelope, input.Snapshot); err != nil {
		return err
	}
	return nil
}

func safeJoin(root, relativeDir, name string) (string, error) {
	path := filepath.Join(root, relativeDir, name)
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("recap: resolve artifact path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("recap: artifact path escapes workspace")
	}
	if err := rejectSymlinkComponents(root, relativeDir); err != nil {
		return "", err
	}
	return path, nil
}

func rejectSymlinkComponents(root, relativeDir string) error {
	current := filepath.Clean(root)
	for _, component := range strings.Split(filepath.Clean(relativeDir), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recap: inspect artifact directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("recap: artifact directory contains a symlink")
		}
	}
	return nil
}

func writeAtomic(ctx context.Context, path string, body []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), artifactDirMode); err != nil {
		return "", fmt.Errorf("recap: create artifact directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".recap-*.tmp")
	if err != nil {
		return "", fmt.Errorf("recap: create temporary artifact: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(artifactFileMode); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("recap: chmod temporary artifact: %w", err)
	}
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("recap: write temporary artifact: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("recap: sync temporary artifact: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("recap: close temporary artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("recap: rename temporary artifact: %w", err)
	}
	removeTemp = false
	return sha256Hex(body), nil
}

func cleanupArtifacts(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func frontMatter(input MaterializeInput, generatedAt time.Time) string {
	return strings.Join([]string{
		"---",
		"recap_id: " + yamlValue(input.RecapID),
		"session_id: " + yamlValue(input.Snapshot.SessionID),
		fmt.Sprintf("source_start_seq: %d", input.Snapshot.SourceStartSeq),
		fmt.Sprintf("source_end_seq: %d", input.Snapshot.SourceEndSeq),
		fmt.Sprintf("source_start_unix_millis: %d", input.Snapshot.SourceStartTime),
		fmt.Sprintf("source_end_unix_millis: %d", input.Snapshot.SourceEndTime),
		"source_end_message_id: " + yamlValue(input.Snapshot.SourceEndMessageID),
		"generated_at_utc: " + generatedAt.Format(time.RFC3339Nano),
		"model: " + yamlValue(input.Model),
		"source_content_sha256: " + sha256Hex(snapshotBytes(input.Snapshot)),
		"---",
		"",
	}, "\n")
}

func questionsMarkdown(questions []Question) string {
	var b strings.Builder
	b.WriteString("# Questions\n\n")
	for _, question := range questions {
		fmt.Fprintf(&b, "- Question: %s\n  Reason: %s\n  Source message IDs: %s\n\n", question.Question, question.Reason, strings.Join(question.SourceMessageIDs, ", "))
	}
	return b.String()
}

func avoidMarkdown(rules []AvoidRule) string {
	var b strings.Builder
	b.WriteString("# Things to avoid\n\n")
	for _, rule := range rules {
		fmt.Fprintf(&b, "- Rule: %s\n  Reason: %s\n  Source message IDs: %s\n\n", rule.Rule, rule.Reason, strings.Join(rule.SourceMessageIDs, ", "))
	}
	return b.String()
}

func snapshotBytes(snapshot Snapshot) []byte {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return []byte{}
	}
	return raw
}

func sha256Hex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func yamlValue(value string) string {
	if safeArtifactID.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}
