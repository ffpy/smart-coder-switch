package trace

import (
	"testing"
	"time"
)

func TestNewRecordDirectoryNameAt(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.July,
		16,
		15,
		12,
		30,
		123456789,
		time.Local,
	)

	actual := newRecordDirectoryNameAt(
		now,
		7,
	)

	expected :=
		"20260716-151230.123456789-000007"

	if actual != expected {
		t.Fatalf(
			"expected %q, got %q",
			expected,
			actual,
		)
	}
}

func TestRecordDirectoryNamesSortByTime(
	t *testing.T,
) {
	first := newRecordDirectoryNameAt(
		time.Date(
			2026,
			time.July,
			16,
			15,
			12,
			30,
			0,
			time.Local,
		),
		1,
	)

	second := newRecordDirectoryNameAt(
		time.Date(
			2026,
			time.July,
			16,
			15,
			12,
			31,
			0,
			time.Local,
		),
		2,
	)

	if first >= second {
		t.Fatalf(
			"expected %q before %q",
			first,
			second,
		)
	}
}
