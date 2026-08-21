package roles

import (
	"reflect"
	"testing"
)

func TestToolsForEveryRole(t *testing.T) {
	tests := []struct {
		role string
		want []string
	}{
		{role: Explore, want: []string{"bash", "read", "grep", "webfetch"}},
		{role: Plan, want: []string{"bash", "read", "grep", "webfetch"}},
		{role: General, want: []string{"bash", "read", "grep", "write", "edit", "webfetch"}},
	}
	for _, tt := range tests {
		if got := Tools(tt.role); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Tools(%q) = %v, want %v", tt.role, got, tt.want)
		}
	}
}
