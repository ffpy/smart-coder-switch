package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func String() string {
	return fmt.Sprintf(
		"smart-coder-switch %s commit=%s built=%s",
		Version,
		Commit,
		BuildTime,
	)
}
