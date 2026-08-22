package recap

import "strings"

// MemorySignalCategory identifies the durable section for an explicit user
// statement. Constraints use decisions because the file has no separate
// constraints section.
type MemorySignalCategory string

const (
	MemorySignalPreference MemorySignalCategory = memorySectionPreferences
	MemorySignalDecision   MemorySignalCategory = memorySectionDecisions
	MemorySignalAvoid      MemorySignalCategory = memorySectionAvoid
	MemorySignalQuestion   MemorySignalCategory = memorySectionQuestions
)

// MemorySignal is a bounded, user-authored instruction extracted before the
// model enriches the memory envelope.
type MemorySignal struct {
	Category         MemorySignalCategory
	Statement        string
	SourceMessageIDs []string
}

// ExtractMemorySignals recognizes explicit user language without treating an
// assistant response as a preference or constraint. It is intentionally
// conservative; ordinary questions and descriptions stay recent context.
func ExtractMemorySignals(snapshot Snapshot) []MemorySignal {
	var signals []MemorySignal
	seen := make(map[string]struct{})
	for _, message := range snapshot.Messages {
		if message.Role != "user" {
			continue
		}
		statement := strings.TrimSpace(message.Text)
		if statement == "" || asksForSecret(statement) || containsSecretMaterial(statement) {
			continue
		}
		categories := classifyMemorySignals(statement)
		if len(categories) == 0 {
			continue
		}
		for _, category := range categories {
			key := string(category) + "\x00" + memoryCanonical(statement)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			signals = append(signals, MemorySignal{
				Category:         category,
				Statement:        truncateString(statement, memoryMaxEntryText),
				SourceMessageIDs: []string{message.ID},
			})
		}
	}
	return signals
}

func classifyMemorySignals(statement string) []MemorySignalCategory {
	text := strings.ToLower(strings.Join(strings.Fields(statement), " "))
	var categories []MemorySignalCategory
	switch {
	case containsAny(text, "i prefer ", "my preference", "always use ", "by default", "i want us to "):
		categories = append(categories, MemorySignalPreference)
	}
	if containsAny(text, "avoid ", "never ", "do not ", "don't ", "must not ", "should not ") {
		categories = append(categories, MemorySignalAvoid)
	}
	if containsAny(text, "we decided", "the decision", "we will use ", "let's use ", "going forward", "must ", "required ", "only ") {
		categories = append(categories, MemorySignalDecision)
	}
	if containsAny(text, "open question", "need to figure out", "unclear", "not sure") {
		categories = append(categories, MemorySignalQuestion)
	}
	return categories
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

// ApplyExplicitMemorySignals restores direct user instructions if the model
// omitted or miscategorized them. The normal envelope validation is rerun so
// source IDs, size limits, duplicates, and secret checks remain enforced.
func ApplyExplicitMemorySignals(envelope MemoryEnvelope, signals []MemorySignal, snapshot Snapshot) (MemoryEnvelope, error) {
	for _, signal := range signals {
		if !validMemorySection(string(signal.Category)) || strings.TrimSpace(signal.Statement) == "" {
			continue
		}
		removeMemoryFact(&envelope, signal.Statement)
		section := memoryFactSection(&envelope, string(signal.Category))
		if section == nil || hasMemoryFact(*section, signal.Statement) {
			continue
		}
		if len(*section) >= memoryMaxEntriesPerPart {
			*section = (*section)[:memoryMaxEntriesPerPart-1]
		}
		*section = append(*section, MemoryFact{
			Text:             signal.Statement,
			Evidence:         "Explicit user instruction from the supplied message.",
			SourceMessageIDs: append([]string{}, signal.SourceMessageIDs...),
		})
	}
	if err := validateMemoryEnvelope(envelope, snapshot); err != nil {
		return MemoryEnvelope{}, err
	}
	return envelope, nil
}

func memoryFactSection(envelope *MemoryEnvelope, category string) *[]MemoryFact {
	switch category {
	case memorySectionPreferences:
		return &envelope.Preferences
	case memorySectionDecisions:
		return &envelope.Decisions
	case memorySectionAvoid:
		return &envelope.ThingsToAvoid
	case memorySectionQuestions:
		return &envelope.Questions
	case memorySectionRecent:
		return &envelope.RecentContext
	default:
		return nil
	}
}

func removeMemoryFact(envelope *MemoryEnvelope, statement string) {
	canonical := memoryCanonical(statement)
	for _, section := range []*[]MemoryFact{
		&envelope.Preferences,
		&envelope.Decisions,
		&envelope.ThingsToAvoid,
		&envelope.Questions,
		&envelope.RecentContext,
	} {
		filtered := (*section)[:0]
		for _, fact := range *section {
			if memoryCanonical(fact.Text) != canonical {
				filtered = append(filtered, fact)
			}
		}
		*section = filtered
	}
}

func hasMemoryFact(section []MemoryFact, statement string) bool {
	canonical := memoryCanonical(statement)
	for _, fact := range section {
		if memoryCanonical(fact.Text) == canonical {
			return true
		}
	}
	return false
}
