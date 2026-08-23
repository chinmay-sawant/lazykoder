package recap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/tools/grep"
)

const maxRecordedMemoryError = 2_000

var memoryWriteLocks sync.Map

// MemoryRunInput joins one durable update reservation to the hidden worker.
type MemoryRunInput struct {
	Store            *db.Store
	Record           db.MemoryUpdate
	Snapshot         Snapshot
	Workdir          string
	Worker           MemoryWorker
	SkillReferences  []MemorySkillReference
	GrepRunner       *grep.Runner
	Enabled          func() bool
	SnapshotDuration time.Duration
}

// RunMemoryUpdate claims, generates, merges, and atomically writes one memory
// update. Any failure is recorded without affecting the parent turn.
func RunMemoryUpdate(ctx context.Context, input MemoryRunInput) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateMemoryRunInput(input); err != nil {
		return err
	}
	stageDurations := make(map[string]int64)
	started := time.Now()
	defer func() {
		recordMemoryStageDuration(stageDurations, "total", started)
		if err := input.Store.RecordMemoryStageDurations(ctx, input.Record.ID, stageDurations); err != nil && runErr == nil {
			runErr = err
		}
	}()
	claimStarted := time.Now()
	if err := input.Store.ClaimMemoryUpdate(ctx, input.Record.ID); err != nil {
		return err
	}
	recordMemoryStageDuration(stageDurations, "claim", claimStarted)
	if input.SnapshotDuration > 0 {
		setMemoryStageDuration(stageDurations, "snapshot_read", input.SnapshotDuration)
	}
	if !memoryStillEnabled(input.Enabled) {
		return input.Store.FailMemoryUpdate(ctx, input.Record.ID, "memory: feature disabled")
	}
	fail := func(err error) error {
		if recordErr := input.Store.FailMemoryUpdate(ctx, input.Record.ID, truncateString(err.Error(), maxRecordedMemoryError)); recordErr != nil {
			return fmt.Errorf("%w: record failure: %w", err, recordErr)
		}
		return err
	}
	if err := validateMemoryRunSnapshot(input.Record, input.Snapshot); err != nil {
		return fail(err)
	}
	documentResult := make(chan memoryDocumentResult, 1)
	go func() {
		readStarted := time.Now()
		loaded, readErr := ReadMemoryDocument(input.Workdir)
		documentResult <- memoryDocumentResult{document: loaded, err: readErr, duration: time.Since(readStarted)}
	}()
	relatedStarted := time.Now()
	related, err := RelatedRecapEvidence(ctx, input.Workdir, input.Snapshot, input.GrepRunner)
	recordMemoryStageDuration(stageDurations, "related_recap_evidence", relatedStarted)
	if err != nil {
		return fail(err)
	}
	loaded := <-documentResult
	setMemoryStageDuration(stageDurations, "aggregate_read", loaded.duration)
	if loaded.err != nil {
		return fail(loaded.err)
	}
	document := loaded.document
	if memoryUpdateIsNoOp(document, input.Snapshot) {
		noopStarted := time.Now()
		current, renderErr := RenderMemoryDocument(document)
		if renderErr != nil {
			return fail(renderErr)
		}
		if completeErr := input.Store.CompleteMemoryUpdate(ctx, input.Record.ID, sha256Hex(current)); completeErr != nil {
			return fail(completeErr)
		}
		recordMemoryStageDuration(stageDurations, "noop", noopStarted)
		return nil
	}
	worker := input.Worker
	if strings.TrimSpace(worker.Model) == "" {
		worker.Model = input.Record.Model
	}
	envelope, workerDurations, err := worker.GenerateWithTimings(ctx, input.Snapshot, document, related)
	for name, duration := range workerDurations {
		stageDurations[name] = duration
	}
	if err != nil {
		return fail(err)
	}
	if !memoryStillEnabled(input.Enabled) {
		return input.Store.FailMemoryUpdate(ctx, input.Record.ID, "memory: feature disabled")
	}
	mergeStarted := time.Now()
	writeLock := memoryLock(input.Workdir)
	writeLock.Lock()
	defer writeLock.Unlock()
	document, err = ReadMemoryDocument(input.Workdir)
	if err != nil {
		recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
		return fail(err)
	}
	if document.LastSessionID == input.Snapshot.SessionID && document.LastMessageSeq > input.Snapshot.SourceEndSeq {
		current, renderErr := RenderMemoryDocument(document)
		if renderErr != nil {
			recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
			return fail(renderErr)
		}
		if completeErr := input.Store.CompleteMemoryUpdate(ctx, input.Record.ID, sha256Hex(current)); completeErr != nil {
			recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
			return fail(completeErr)
		}
		recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
		return nil
	}
	merged, err := MergeMemoryWithSkills(document, envelope, input.Snapshot, input.SkillReferences, timeNow())
	if err != nil {
		recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
		return fail(err)
	}
	digest, err := WriteMemoryDocument(ctx, input.Workdir, merged)
	if err != nil {
		recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
		return fail(err)
	}
	if err := input.Store.CompleteMemoryUpdate(ctx, input.Record.ID, digest); err != nil {
		recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
		return fail(err)
	}
	recordMemoryStageDuration(stageDurations, "merge_write", mergeStarted)
	return nil
}

func memoryLock(workdir string) *sync.Mutex {
	lock, _ := memoryWriteLocks.LoadOrStore(workdir, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func validateMemoryRunInput(input MemoryRunInput) error {
	if input.Store == nil {
		return errors.New("memory: run store is required")
	}
	if strings.TrimSpace(input.Record.ID) == "" {
		return errors.New("memory: run record ID is required")
	}
	if strings.TrimSpace(input.Record.SourceSessionID) == "" {
		return errors.New("memory: run source session is required")
	}
	if strings.TrimSpace(input.Workdir) == "" {
		return errors.New("memory: run workdir is required")
	}
	return nil
}

func validateMemoryRunSnapshot(record db.MemoryUpdate, snapshot Snapshot) error {
	if record.SourceSessionID != snapshot.SessionID {
		return errors.New("memory: reserved session does not match snapshot")
	}
	if record.SourceEndSeq != snapshot.SourceEndSeq {
		return errors.New("memory: reserved source sequence does not match snapshot")
	}
	if record.SourceEndMessageID != snapshot.SourceEndMessageID {
		return errors.New("memory: reserved source message does not match snapshot")
	}
	return nil
}

func memoryStillEnabled(enabled func() bool) bool {
	return enabled == nil || enabled()
}

func memoryUpdateIsNoOp(document MemoryDocument, snapshot Snapshot) bool {
	if document.LastSessionID != snapshot.SessionID || document.LastMessageSeq <= 0 {
		return false
	}
	if len(ExtractMemorySignals(snapshot)) > 0 {
		return false
	}
	if document.LastMessageSeq >= snapshot.SourceEndSeq {
		return true
	}
	for _, message := range snapshot.Messages {
		if message.Seq > document.LastMessageSeq && message.Role == "assistant" && strings.TrimSpace(message.Text) != "" {
			return false
		}
	}
	return true
}

type memoryDocumentResult struct {
	document MemoryDocument
	err      error
	duration time.Duration
}

func RelatedRecapEvidence(ctx context.Context, workdir string, snapshot Snapshot, runner *grep.Runner) (string, error) {
	pattern := relatedPattern(snapshot)
	if pattern == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	searchCtx, cancel := context.WithTimeout(ctx, relatedAvoidTimeout)
	defer cancel()
	result, err := grep.Run(searchCtx, workdir, grep.Options{
		Pattern:         pattern,
		Path:            "knowledge-base/recaps",
		Glob:            "*.md",
		CaseInsensitive: true,
		MaxMatches:      maxRelatedMatches,
	}, runner)
	if err == nil && strings.TrimSpace(result.Output) != "" && strings.TrimSpace(result.Output) != "no matches" {
		return truncateString(result.Output, maxRelatedOutput), nil
	}
	result, err = grep.Run(searchCtx, workdir, grep.Options{
		Pattern:         pattern,
		Path:            "knowledge-base",
		Glob:            "*.md",
		CaseInsensitive: true,
		MaxMatches:      maxRelatedMatches,
	}, runner)
	if err != nil || strings.TrimSpace(result.Output) == "" || strings.TrimSpace(result.Output) == "no matches" {
		return "", nil
	}
	return truncateString(result.Output, maxRelatedOutput), nil
}

func timeNow() time.Time {
	return time.Now()
}
