package agent

import "testing"

func TestBaseToolRegistryHasMatchingSpecsAndRunners(t *testing.T) {
	for name, registration := range baseToolRegistry {
		if registration.spec.Name != name {
			t.Errorf("tool %q has spec name %q", name, registration.spec.Name)
		}
		if registration.runner == nil {
			t.Errorf("tool %q has no runner", name)
		}
	}
}
