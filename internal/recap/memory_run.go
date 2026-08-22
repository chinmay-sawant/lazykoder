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
	Store      *db.Store
	Record     db.MemoryUpdate
	Snapshot   Snapshot
	Workdir    string
	Worker     MemoryWorker
	GrepRunner *grep.Runner
	Enabled    func() bool
}

// RunMemoryUpdate claims, generates, merges, and atomically writes one memory
// update. Any failure is recorded without affecting the parent turn.
func RunMemoryUpdate(ctx context.Context, input MemoryRunInput) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateMemoryRunInput(input); err != nil {
		return err
	}
	if err := input.Store.ClaimMemoryUpdate(ctx, input.Record.ID); err != nil {
		return err
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
	document, err := ReadMemoryDocument(input.Workdir)
	if err != nil {
		return fail(err)
	}
	related, err := RelatedRecapEvidence(ctx, input.Workdir, input.Snapshot, input.GrepRunner)
	if err != nil {
		return fail(err)
	}
	worker := input.Worker
	if strings.TrimSpace(worker.Model) == "" {
		worker.Model = input.Record.Model
	}
	envelope, err := worker.Generate(ctx, input.Snapshot, document, related)
	if err != nil {
		return fail(err)
	}
	if !memoryStillEnabled(input.Enabled) {
		return input.Store.FailMemoryUpdate(ctx, input.Record.ID, "memory: feature disabled")
	}
	writeLock := memoryLock(input.Workdir)
	writeLock.Lock()
	defer writeLock.Unlock()
	document, err = ReadMemoryDocument(input.Workdir)
	if err != nil {
		return fail(err)
	}
	if document.LastSessionID == input.Snapshot.SessionID && document.LastMessageSeq > input.Snapshot.SourceEndSeq {
		current, renderErr := RenderMemoryDocument(document)
		if renderErr != nil {
			return fail(renderErr)
		}
		if completeErr := input.Store.CompleteMemoryUpdate(ctx, input.Record.ID, sha256Hex(current)); completeErr != nil {
			return fail(completeErr)
		}
		return nil
	}
	merged, err := MergeMemory(document, envelope, input.Snapshot, timeNow())
	if err != nil {
		return fail(err)
	}
	digest, err := WriteMemoryDocument(ctx, input.Workdir, merged)
	if err != nil {
		return fail(err)
	}
	if err := input.Store.CompleteMemoryUpdate(ctx, input.Record.ID, digest); err != nil {
		return fail(err)
	}
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
