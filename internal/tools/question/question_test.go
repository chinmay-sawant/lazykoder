package question

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunOneQuestion(t *testing.T) {
	var calls int
	res, err := Run([]Question{{Header: "h", Question: "pick", Options: []string{"a", "b"}}}, func(Question) (int, error) {
		calls++
		return 0, nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Output != "answered 1 question(s)" {
		t.Errorf("Output = %q, want %q", res.Output, "answered 1 question(s)")
	}
	if got := res.Metadata["answers"]; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("answers = %#v, want [a]", got)
	}
	if got := res.Metadata["indexes"]; !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("indexes = %#v, want [0]", got)
	}
	if res.Metadata["questions"] != 1 {
		t.Errorf("questions = %v, want 1", res.Metadata["questions"])
	}
	if calls != 1 {
		t.Errorf("ask calls = %d, want 1", calls)
	}
}

func TestRunTwoQuestionsInOrder(t *testing.T) {
	var order []string
	res, err := Run([]Question{
		{Header: "h1", Question: "first", Options: []string{"a", "b"}},
		{Header: "h2", Question: "second", Options: []string{"c", "d", "e"}},
	}, func(q Question) (int, error) {
		order = append(order, q.Question)
		if q.Question == "first" {
			return 1, nil
		}
		return 2, nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"first", "second"}; !reflect.DeepEqual(order, want) {
		t.Errorf("ask order = %v, want %v", order, want)
	}
	if got := res.Metadata["answers"]; !reflect.DeepEqual(got, []string{"b", "e"}) {
		t.Errorf("answers = %#v, want [b e]", got)
	}
	if got := res.Metadata["indexes"]; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("indexes = %#v, want [1 2]", got)
	}
	if res.Output != "answered 2 question(s)" {
		t.Errorf("Output = %q, want %q", res.Output, "answered 2 question(s)")
	}
}

func TestRunInvalidIndex(t *testing.T) {
	tests := []struct {
		name string
		idx  int
	}{
		{"negative", -1},
		{"out of range", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run([]Question{{Question: "pick", Options: []string{"a", "b"}}}, func(Question) (int, error) {
				return tt.idx, nil
			})
			if err == nil {
				t.Fatal("Run() error = nil, want out-of-range error")
			}
			if !strings.Contains(err.Error(), "question:") {
				t.Errorf("error = %q, want question: prefix", err)
			}
		})
	}
}

func TestRunAskErrorAborts(t *testing.T) {
	sentinel := errors.New("ask blew up")
	var calls int
	_, err := Run([]Question{{Question: "q", Options: []string{"a"}}}, func(Question) (int, error) {
		calls++
		return 0, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Run() error = %v, want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), "question:") {
		t.Errorf("error = %q, want question: prefix", err)
	}
	if calls != 1 {
		t.Errorf("ask calls = %d, want 1", calls)
	}
}

func TestRunEmpty(t *testing.T) {
	var calls int
	res, err := Run(nil, func(Question) (int, error) {
		calls++
		return 0, nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Output != "answered 0 question(s)" {
		t.Errorf("Output = %q, want %q", res.Output, "answered 0 question(s)")
	}
	if calls != 0 {
		t.Errorf("ask calls = %d, want 0", calls)
	}
	if got := res.Metadata["answers"]; len(got.([]string)) != 0 {
		t.Errorf("answers = %#v, want empty", got)
	}
	if res.Metadata["questions"] != 0 {
		t.Errorf("questions = %v, want 0", res.Metadata["questions"])
	}
}
