package recap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/tools/grep"
)

const maxRecordedRecapError = 2_000

// RunInput joins one Phase 1 reservation to the hidden worker and local
// materializer. The caller captures Snapshot before reserving the record.
type RunInput struct {
	Store      *db.Store
	Record     db.RecapRecord
	Snapshot   Snapshot
	Workdir    string
	Worker     Worker
	GrepRunner *grep.Runner
	// Enabled is checked at each side-effect boundary. A disabled feature may
	// let an in-flight provider request finish, but it cannot materialize files.
	Enabled func() bool
}

// Run claims, generates, materializes, and completes one reserved recap. Any
// post-claim failure is recorded in the ledger and returned to the caller.
func Run(ctx context.Context, input RunInput) (ArtifactManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRunInput(input); err != nil {
		return ArtifactManifest{}, err
	}
	if err := input.Store.ClaimRecap(ctx, input.Record.ID); err != nil {
		return ArtifactManifest{}, err
	}
	if !recapStillEnabled(input.Enabled) {
		_ = input.Store.CancelRecap(ctx, input.Record.ID)
		return ArtifactManifest{}, nil
	}
	fail := func(err error) (ArtifactManifest, error) {
		if recordErr := input.Store.FailRecap(ctx, input.Record.ID, truncateString(err.Error(), maxRecordedRecapError)); recordErr != nil {
			return ArtifactManifest{}, fmt.Errorf("%w: record failure: %w", err, recordErr)
		}
		return ArtifactManifest{}, err
	}
	if err := validateRunSnapshot(input.Record, input.Snapshot); err != nil {
		return fail(err)
	}
	worker := input.Worker
	if strings.TrimSpace(worker.Model) == "" {
		worker.Model = input.Record.Model
	}
	relatedAvoid, err := RelatedAvoid(ctx, input.Workdir, input.Snapshot, input.GrepRunner)
	if err != nil {
		return fail(err)
	}
	envelope, err := worker.Generate(ctx, input.Snapshot, relatedAvoid)
	if err != nil {
		return fail(err)
	}
	if !recapStillEnabled(input.Enabled) {
		_ = input.Store.CancelRecap(ctx, input.Record.ID)
		return ArtifactManifest{}, nil
	}
	manifest, err := Materialize(ctx, input.Workdir, MaterializeInput{
		RecapID:  input.Record.ID,
		Model:    input.Record.Model,
		Snapshot: input.Snapshot,
		Envelope: envelope,
	})
	if err != nil {
		return fail(err)
	}
	if err := input.Store.CompleteRecap(ctx, input.Record.ID, manifest.DBArtifacts()); err != nil {
		return fail(err)
	}
	return manifest, nil
}

func recapStillEnabled(enabled func() bool) bool {
	return enabled == nil || enabled()
}

func validateRunInput(input RunInput) error {
	if input.Store == nil {
		return errors.New("recap: run store is required")
	}
	if strings.TrimSpace(input.Record.ID) == "" {
		return errors.New("recap: run record ID is required")
	}
	if strings.TrimSpace(input.Record.SessionID) == "" {
		return errors.New("recap: run record session ID is required")
	}
	if strings.TrimSpace(input.Workdir) == "" {
		return errors.New("recap: run workdir is required")
	}
	return nil
}

func validateRunSnapshot(record db.RecapRecord, snapshot Snapshot) error {
	if record.SessionID != snapshot.SessionID {
		return errors.New("recap: reserved session does not match snapshot")
	}
	if record.SourceStartSeq != snapshot.SourceStartSeq || record.SourceEndSeq != snapshot.SourceEndSeq {
		return errors.New("recap: reserved source range does not match snapshot")
	}
	if record.SourceStartTime != snapshot.SourceStartTime || record.SourceEndTime != snapshot.SourceEndTime {
		return errors.New("recap: reserved source times do not match snapshot")
	}
	if record.SourceEndMessageID != snapshot.SourceEndMessageID {
		return errors.New("recap: reserved source end message does not match snapshot")
	}
	return nil
}
