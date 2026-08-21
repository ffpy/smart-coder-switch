package trace

import (
	"fmt"
	"sync/atomic"
	"time"
)

var recordDirectorySequence uint64

func newRecordDirectoryName() string {
	sequence := atomic.AddUint64(
		&recordDirectorySequence,
		1,
	)

	return newRecordDirectoryNameAt(
		time.Now(),
		sequence,
	)
}

func newRecordDirectoryNameAt(
	now time.Time,
	sequence uint64,
) string {
	return fmt.Sprintf(
		"%s-%06d",
		now.Format(
			"20060102-150405.000000000",
		),
		sequence,
	)
}
