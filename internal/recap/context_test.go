package recap

import (
	"context"
	"errors"
	"testing"
)

func TestContextAwareEntriesRejectNilContext(t *testing.T) {
	root := t.TempDir()
	snapshot := workerTestSnapshot()
	worker := Worker{Model: "test"}
	memoryWorker := MemoryWorker{Model: "test"}
	var nilContext context.Context

	tests := []struct {
		name string
		call func() error
	}{
		{name: "worker", call: func() error {
			_, err := worker.Generate(nilContext, snapshot, "")
			return err
		}},
		{name: "memory worker", call: func() error {
			_, err := memoryWorker.Generate(nilContext, snapshot, NewMemoryDocument(), "")
			return err
		}},
		{name: "related avoid", call: func() error {
			_, err := RelatedAvoid(nilContext, root, snapshot, nil)
			return err
		}},
		{name: "related memory evidence", call: func() error {
			_, err := RelatedRecapEvidence(nilContext, root, snapshot, nil)
			return err
		}},
		{name: "materialize", call: func() error {
			_, err := Materialize(nilContext, root, MaterializeInput{})
			return err
		}},
		{name: "write memory", call: func() error {
			_, err := WriteMemoryDocument(nilContext, root, NewMemoryDocument())
			return err
		}},
		{name: "run", call: func() error {
			_, err := Run(nilContext, RunInput{})
			return err
		}},
		{name: "memory run", call: func() error {
			return RunMemoryUpdate(nilContext, MemoryRunInput{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrNilContext) {
				t.Fatalf("error = %v, want ErrNilContext", err)
			}
		})
	}
}
