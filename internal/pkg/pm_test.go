package pkg

import (
	"reflect"
	"testing"
)

func TestDevDependencyArgs(t *testing.T) {
	cases := []struct {
		pm   string
		want []string
	}{
		{"bun", []string{"add", "-d", "vitest"}},
		{"pnpm", []string{"add", "-D", "vitest"}},
		{"yarn", []string{"add", "-D", "vitest"}},
		{"npm", []string{"install", "-D", "vitest"}},
	}
	for _, tc := range cases {
		got := DevDependencyArgs(tc.pm, "vitest")
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("DevDependencyArgs(%q) = %v, want %v", tc.pm, got, tc.want)
		}
	}
	if got := DevDependencyArgs("unknown", "vitest"); got != nil {
		t.Errorf("DevDependencyArgs(unknown) = %v, want nil", got)
	}
}
