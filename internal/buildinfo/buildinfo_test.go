package buildinfo

import (
	"strings"
	"testing"
)

func TestString(
	t *testing.T,
) {
	oldVersion := Version
	oldCommit := Commit
	oldBuildTime := BuildTime

	t.Cleanup(
		func() {
			Version = oldVersion
			Commit = oldCommit
			BuildTime = oldBuildTime
		},
	)

	Version = "1.2.3"
	Commit = "abc123"
	BuildTime = "2026-07-16T14:00:00Z"

	result := String()

	expectedParts := []string{
		"smart-coder-switch 1.2.3",
		"commit=abc123",
		"built=2026-07-16T14:00:00Z",
	}

	for _, expected := range expectedParts {
		if !strings.Contains(
			result,
			expected,
		) {
			t.Fatalf(
				"expected %q in %q",
				expected,
				result,
			)
		}
	}
}
