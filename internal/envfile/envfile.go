// Package envfile loads KEY=VALUE pairs from a dotenv-style file without
// overriding variables already present in the process environment.
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// Load reads path and sets any variable that is not already set in the
// process environment. A missing file is not an error. Malformed lines
// are skipped. Values may be wrapped in single or double quotes; the
// optional "export" prefix is accepted. No variable expansion is done.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	existing := map[string]bool{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		existing[key] = true
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if existing[key] {
			continue
		}
		_ = os.Setenv(key, value)
		existing[key] = true
	}
	return sc.Err()
}
