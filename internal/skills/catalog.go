// Package skills discovers and reads bounded skill descriptors.
package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	ScopeLocal  Scope = "local"
	ScopeGlobal Scope = "global"

	DescriptorName       = "SKILL.md"
	LegacyDescriptorName = "SKILLS.md"

	DefaultMaxDepth          = 4
	DefaultMaxSkills         = 256
	DefaultMaxDescriptorSize = 256 * 1024
	DefaultMaxBodySize       = 48 * 1024
	DefaultMaxMatches        = 3
	MaxMaxMatches            = 12
	MaxMaxSkills             = 512
	maxSkillHeadings         = 12
	maxSkillDescription      = 600
	maxPromptTerms           = 12
	minPromptTermRunes       = 2

	globalSkillsEnv = "LAZYKODER_GLOBAL_SKILLS_DIR"

	pathDisplayHome = "~"
)

// Scope identifies where a skill was found.
type Scope string

// Root is one approved directory for skill discovery.
type Root struct {
	Scope    Scope
	Label    string
	Path     string
	Priority int
}

// Diagnostic records a root or descriptor that could not be used.
type Diagnostic struct {
	Scope Scope
	Path  string
	Error string
}

// Skill is bounded metadata for one skill descriptor.
type Skill struct {
	Name           string
	Description    string
	Triggers       []string
	Headings       []string
	Scope          Scope
	RootLabel      string
	Path           string
	DisplayPath    string
	DescriptorPath string
	ContentHash    string
}

// Match is a ranked catalog result with human-readable reasons.
type Match struct {
	Skill   Skill
	Score   int
	Reasons []string
}

// Context is the bounded request-time form of a selected skill.
// Body is wire-only and must not be written to the chat transcript.
type Context struct {
	Name        string
	Description string
	Scope       Scope
	Path        string
	ContentHash string
	Reasons     []string
	Body        string
}

// Catalog is a deterministic snapshot of the approved skill roots.
type Catalog struct {
	Skills      []Skill
	Diagnostics []Diagnostic
	ScannedAt   time.Time
}

// Options bounds one discovery pass.
type Options struct {
	Workdir        string
	IncludeLocal   bool
	IncludeGlobal  bool
	GlobalRoots    []string
	MaxDepth       int
	MaxSkills      int
	MaxDescriptor  int
	MaxBody        int
	MaxAutoMatches int
}

// DefaultOptions returns the safe discovery limits.
func DefaultOptions(workdir string) Options {
	return Options{
		Workdir:        workdir,
		IncludeLocal:   true,
		IncludeGlobal:  true,
		MaxDepth:       DefaultMaxDepth,
		MaxSkills:      DefaultMaxSkills,
		MaxDescriptor:  DefaultMaxDescriptorSize,
		MaxBody:        DefaultMaxBodySize,
		MaxAutoMatches: DefaultMaxMatches,
	}
}

// ResolveRoots returns only the approved local and global root candidates.
// Missing roots are omitted. Invalid existing roots are returned as diagnostics.
func ResolveRoots(workdir string, includeLocal, includeGlobal bool) ([]Root, []Diagnostic) {
	return resolveRoots(workdir, includeLocal, includeGlobal, nil)
}

func resolveRoots(workdir string, includeLocal, includeGlobal bool, explicitGlobal []string) ([]Root, []Diagnostic) {
	var roots []Root
	var diagnostics []Diagnostic
	seen := make(map[string]struct{})
	add := func(scope Scope, label, path string, priority int) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: path, Error: err.Error()})
			return
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		info, err := os.Lstat(abs)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: abs, Error: err.Error()})
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: abs, Error: "root is not a real directory"})
			return
		}
		roots = append(roots, Root{Scope: scope, Label: label, Path: abs, Priority: priority})
	}
	if includeLocal {
		add(ScopeLocal, "project skills", filepath.Join(workdir, "skills"), 0)
		add(ScopeLocal, "project .agents skills", filepath.Join(workdir, ".agents", "skills"), 1)
	}
	if includeGlobal {
		priority := 10
		if len(explicitGlobal) > 0 {
			for _, path := range explicitGlobal {
				add(ScopeGlobal, globalSkillsEnv, path, priority)
				priority++
			}
		} else {
			for _, path := range splitPathList(os.Getenv(globalSkillsEnv)) {
				add(ScopeGlobal, globalSkillsEnv, path, priority)
				priority++
			}
			if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
				add(ScopeGlobal, "CODEX_HOME", filepath.Join(codexHome, "skills"), priority)
				priority++
			}
			if home, err := os.UserHomeDir(); err == nil {
				add(ScopeGlobal, "home .agents", filepath.Join(home, ".agents", "skills"), priority)
			}
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].Priority != roots[j].Priority {
			return roots[i].Priority < roots[j].Priority
		}
		return roots[i].Path < roots[j].Path
	})
	return roots, diagnostics
}

// Discover scans the approved roots and returns a partial catalog on a root
// or descriptor failure. Context cancellation remains fatal to the pass.
func Discover(ctx context.Context, opts Options) (Catalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.Workdir) == "" {
		return Catalog{}, errors.New("skills: workdir is required")
	}
	applyDefaults(&opts)
	roots, diagnostics := resolveRoots(opts.Workdir, opts.IncludeLocal, opts.IncludeGlobal, opts.GlobalRoots)
	catalog := Catalog{Skills: []Skill{}, Diagnostics: diagnostics, ScannedAt: time.Now().UTC()}
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return catalog, err
		}
		found, rootDiagnostics := scanRoot(ctx, opts, root)
		catalog.Skills = append(catalog.Skills, found...)
		catalog.Diagnostics = append(catalog.Diagnostics, rootDiagnostics...)
		if len(catalog.Skills) >= opts.MaxSkills {
			catalog.Skills = catalog.Skills[:opts.MaxSkills]
			break
		}
	}
	for index := range catalog.Skills {
		catalog.Skills[index].Triggers = append([]string{}, catalog.Skills[index].Triggers...)
		catalog.Skills[index].Headings = append([]string{}, catalog.Skills[index].Headings...)
	}
	sortSkills(catalog.Skills)
	return catalog, nil
}

// ReadBody reads one previously discovered descriptor with a bounded size.
func ReadBody(ctx context.Context, skill Skill, maxBytes int) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(skill.DescriptorPath) == "" {
		return "", errors.New("skills: descriptor path is required")
	}
	if maxBytes <= 0 || maxBytes > DefaultMaxBodySize {
		maxBytes = DefaultMaxBodySize
	}
	info, err := os.Lstat(skill.DescriptorPath)
	if err != nil {
		return "", fmt.Errorf("skills: stat descriptor: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("skills: descriptor is not a regular file")
	}
	data, err := os.ReadFile(skill.DescriptorPath)
	if err != nil {
		return "", fmt.Errorf("skills: read descriptor: %w", err)
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data), nil
}

// Query ranks all catalog entries for a human search query.
func (c Catalog) Query(query string, max int) []Match {
	terms := promptTerms(query)
	if len(terms) == 0 {
		matches := make([]Match, 0, len(c.Skills))
		for _, skill := range c.Skills {
			matches = append(matches, Match{Skill: skill})
		}
		if max > 0 && len(matches) > max {
			matches = matches[:max]
		}
		return matches
	}
	matches := make([]Match, 0, len(c.Skills))
	for _, skill := range c.Skills {
		match := scoreSkill(skill, terms)
		if match.Score > 0 {
			matches = append(matches, match)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].Skill.Scope != matches[j].Skill.Scope {
			return matches[i].Skill.Scope == ScopeLocal
		}
		return skillSortKey(matches[i].Skill) < skillSortKey(matches[j].Skill)
	})
	if max > 0 && len(matches) > max {
		matches = matches[:max]
	}
	return matches
}

// AutoMatches ranks matches and keeps the first local copy for duplicate names.
func (c Catalog) AutoMatches(query string, max int) []Match {
	if max <= 0 || max > MaxMaxMatches {
		max = DefaultMaxMatches
	}
	all := c.Query(query, 0)
	byName := make(map[string]Match, len(all))
	for _, match := range all {
		key := normalize(match.Skill.Name)
		previous, exists := byName[key]
		if !exists || (match.Skill.Scope == ScopeLocal && previous.Skill.Scope != ScopeLocal) {
			byName[key] = match
		}
	}
	ordered := make([]Match, 0, len(byName))
	for _, match := range all {
		key := normalize(match.Skill.Name)
		if chosen, ok := byName[key]; ok && chosen.Skill.DescriptorPath == match.Skill.DescriptorPath {
			ordered = append(ordered, match)
			delete(byName, key)
		}
	}
	out := make([]Match, 0, max)
	for _, match := range ordered {
		out = append(out, match)
		if len(out) == max {
			break
		}
	}
	return out
}

func applyDefaults(opts *Options) {
	if opts.MaxDepth <= 0 || opts.MaxDepth > 8 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxSkills <= 0 || opts.MaxSkills > MaxMaxSkills {
		opts.MaxSkills = DefaultMaxSkills
	}
	if opts.MaxDescriptor <= 0 || opts.MaxDescriptor > 1<<20 {
		opts.MaxDescriptor = DefaultMaxDescriptorSize
	}
	if opts.MaxBody <= 0 || opts.MaxBody > 1<<20 {
		opts.MaxBody = DefaultMaxBodySize
	}
	if opts.MaxAutoMatches <= 0 || opts.MaxAutoMatches > MaxMaxMatches {
		opts.MaxAutoMatches = DefaultMaxMatches
	}
}

func scanRoot(ctx context.Context, opts Options, root Root) ([]Skill, []Diagnostic) {
	var skills []Skill
	var diagnostics []Diagnostic
	err := filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: path, Error: walkErr.Error()})
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root.Path && entry.Type()&os.ModeSymlink != 0 {
				return fs.SkipDir
			}
			if skillDepth(root.Path, path) > opts.MaxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		name := entry.Name()
		if name != DescriptorName && name != LegacyDescriptorName {
			return nil
		}
		if len(skills) >= opts.MaxSkills {
			return fs.SkipDir
		}
		data, err := os.ReadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: path, Error: err.Error()})
			return continueWalkAfterDiagnostic()
		}
		if len(data) > opts.MaxDescriptor {
			diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: path, Error: "descriptor exceeds size limit"})
			return nil
		}
		skill, err := parseDescriptor(root, path, data, opts.Workdir)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: path, Error: err.Error()})
			return continueWalkAfterDiagnostic()
		}
		if name == LegacyDescriptorName && siblingExists(path, DescriptorName) {
			diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: path, Error: "ignored because SKILL.md exists"})
			return nil
		}
		skills = append(skills, skill)
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		diagnostics = append(diagnostics, Diagnostic{Scope: root.Scope, Path: root.Path, Error: err.Error()})
	}
	return skills, diagnostics
}

// continueWalkAfterDiagnostic keeps sibling skill descriptors visible after
// one descriptor cannot be read or parsed.
func continueWalkAfterDiagnostic() error {
	return nil
}

func parseDescriptor(root Root, path string, data []byte, workdir string) (Skill, error) {
	text := string(data)
	if strings.TrimSpace(firstLine(text)) == "---" && !hasFrontMatterEnd(text) {
		return Skill{}, errors.New("skill front matter is not terminated")
	}
	fields, body := parseFrontMatter(text)
	name := strings.TrimSpace(fields["name"])
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Skill{}, errors.New("skill name is empty")
	}
	description := strings.TrimSpace(fields["description"])
	if description == "" {
		description = firstParagraph(body)
	}
	headings := extractHeadings(body, maxSkillHeadings)
	triggers := splitList(fields["triggers"])
	if len(triggers) == 0 {
		triggers = append([]string{}, splitList(fields["keywords"])...)
	}
	digest := sha256.Sum256(data)
	display := displayPath(path, root.Scope, workdir)
	return Skill{
		Name:           name,
		Description:    truncate(description, maxSkillDescription),
		Triggers:       cleanList(triggers),
		Headings:       cleanList(headings),
		Scope:          root.Scope,
		RootLabel:      root.Label,
		Path:           filepath.Clean(filepath.Dir(path)),
		DisplayPath:    display,
		DescriptorPath: filepath.Clean(path),
		ContentHash:    hex.EncodeToString(digest[:]),
	}, nil
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func hasFrontMatterEnd(text string) bool {
	lines := strings.Split(text, "\n")
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			return true
		}
	}
	return false
}

func parseFrontMatter(text string) (map[string]string, string) {
	fields := make(map[string]string)
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fields, text
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end < 0 {
		return fields, text
	}
	for index := 1; index < end; index++ {
		line := strings.TrimSpace(lines[index])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		if value == ">" || value == "|" {
			var continuation []string
			for next := index + 1; next < end; next++ {
				if strings.TrimSpace(lines[next]) == "" {
					continuation = append(continuation, "")
					index = next
					continue
				}
				if len(lines[next]) == 0 || (lines[next][0] != ' ' && lines[next][0] != '\t') {
					break
				}
				continuation = append(continuation, strings.TrimSpace(lines[next]))
				index = next
			}
			if value == ">" {
				value = strings.Join(continuation, " ")
			} else {
				value = strings.Join(continuation, "\n")
			}
		}
		fields[key] = strings.TrimSpace(strings.Trim(value, "\"'"))
	}
	return fields, strings.Join(lines[end+1:], "\n")
}

func scoreSkill(skill Skill, terms []string) Match {
	name := normalize(skill.Name)
	description := normalize(skill.Description)
	triggerText := normalize(strings.Join(skill.Triggers, " "))
	headingText := normalize(strings.Join(skill.Headings, " "))
	match := Match{Skill: skill}
	for _, term := range terms {
		switch {
		case name == term:
			match.Score += 100
			match.Reasons = append(match.Reasons, "name")
		case strings.HasPrefix(name, term):
			match.Score += 60
			match.Reasons = append(match.Reasons, "name prefix")
		case strings.Contains(triggerText, term):
			match.Score += 50
			match.Reasons = append(match.Reasons, "trigger")
		case strings.Contains(description, term):
			match.Score += 25
			match.Reasons = append(match.Reasons, "description")
		case strings.Contains(headingText, term):
			match.Score += 15
			match.Reasons = append(match.Reasons, "heading")
		}
	}
	return match
}

func promptTerms(text string) []string {
	stop := map[string]struct{}{"a": {}, "an": {}, "and": {}, "for": {}, "in": {}, "of": {}, "on": {}, "the": {}, "to": {}, "with": {}}
	seen := make(map[string]struct{})
	terms := make([]string, 0, maxPromptTerms)
	for _, value := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
	}) {
		value = strings.Trim(value, "_-")
		if len([]rune(value)) < minPromptTermRunes {
			continue
		}
		if _, ok := stop[value]; ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		terms = append(terms, value)
		if len(terms) == maxPromptTerms {
			break
		}
	}
	return terms
}

func splitPathList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.FieldsFunc(value, func(r rune) bool { return r == os.PathListSeparator || r == '\n' })
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	return strings.Split(value, ",")
}

func cleanList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.Trim(value, "\"'"))
		if value == "" {
			continue
		}
		key := normalize(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstParagraph(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, ">"))
	}
	return ""
}

func extractHeadings(text string, max int) []string {
	headings := make([]string, 0, max)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") || len(headings) == max {
			continue
		}
		headings = append(headings, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
	}
	return headings
}

func displayPath(path string, scope Scope, workdir string) string {
	if scope == ScopeLocal {
		if rel, err := filepath.Rel(workdir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return pathDisplayHome + "/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func skillDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func siblingExists(path, name string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(path), name))
	return err == nil
}

func sortSkills(values []Skill) {
	sort.SliceStable(values, func(i, j int) bool {
		return skillSortKey(values[i]) < skillSortKey(values[j])
	})
}

func skillSortKey(skill Skill) string {
	return normalize(skill.Name) + "\x00" + string(skill.Scope) + "\x00" + skill.DisplayPath
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func truncate(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
