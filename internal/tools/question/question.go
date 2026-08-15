package question

import (
	"fmt"
)

// Question is a single prompt with selectable options.
type Question struct {
	Header   string
	Question string
	Options  []string
}

// Result summarizes the collected answers.
type Result struct {
	Output   string
	Metadata map[string]any
}

// Run asks each question in order through ask and collects the chosen options.
func Run(questions []Question, ask func(Question) (int, error)) (Result, error) {
	answers := make([]string, 0, len(questions))
	indexes := make([]int, 0, len(questions))
	for _, q := range questions {
		idx, err := ask(q)
		if err != nil {
			return Result{}, fmt.Errorf("question: %w", err)
		}
		if idx < 0 || idx >= len(q.Options) {
			return Result{}, fmt.Errorf("question: ask returned index %d out of range [0, %d)", idx, len(q.Options))
		}
		answers = append(answers, q.Options[idx])
		indexes = append(indexes, idx)
	}
	return Result{
		Output: fmt.Sprintf("answered %d question(s)", len(questions)),
		Metadata: map[string]any{
			"answers":   answers,
			"indexes":   indexes,
			"questions": len(questions),
		},
	}, nil
}
