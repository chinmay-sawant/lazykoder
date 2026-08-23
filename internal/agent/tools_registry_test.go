package agent

import "testing"

func TestBaseToolRegistryHasMatchingSpecsAndRunners(t *testing.T) {
	if err := validateBaseToolRegistry(); err != nil {
		t.Fatal(err)
	}
}
