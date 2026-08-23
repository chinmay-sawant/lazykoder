package recap

import (
	"errors"
	"testing"
)

func TestContextAwareEntriesRejectNilContext(t *testing.T) {
	root := t.TempDir()
	snapshot := workerTestSnapshot()
	worker := Worker{Model: "test"}
	memoryWorker := MemoryWorker{Model: "test"}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "worker", call: func() error {
			_, err := worker.Generate(nil, snapshot, "")
			return err
		}},
		{name: "memory worker", call: func() error {
			_, err := memoryWorker.Generate(nil, snapshot, NewMemoryDocument(), "")
			return err
		}},
		{name: "related avoid", call: func() error {
			_, err := RelatedAvoid(nil, root, snapshot, nil)
			return err
		}},
		{name: "related memory evidence", call: func() error {
			_, err := RelatedRecapEvidence(nil, root, snapshot, nil)
			return err
		}},
		{name: "materialize", call: func() error {
			_, err := Materialize(nil, root, MaterializeInput{})
			return err
		}},
		{name: "write memory", call: func() error {
			_, err := WriteMemoryDocument(nil, root, NewMemoryDocument())
			return err
		}},
		{name: "run", call: func() error {
			_, err := Run(nil, RunInput{})
			return err
		}},
		{name: "memory run", call: func() error {
			return RunMemoryUpdate(nil, MemoryRunInput{})
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
