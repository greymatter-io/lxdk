package version

import "fmt"

var (
	commit  string
	version string
	branch  string
)

func Version() string {
	ret := fmt.Sprintf("%s %s", version, commit)
	if version == "" {
		ret = fmt.Sprintf("%s %s", "UNRELEASED", commit)
	}
	return ret
}
