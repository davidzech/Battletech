package site

import (
	"os"
	"runtime/debug"
	"strings"
)

// buildCommit is the git commit the server was built from: the revision Go stamps into
// `go build` binaries when .git is present, else $COMMIT_HASH (set it on DigitalOcean App
// Platform to ${_self.COMMIT_HASH}, which covers buildpack and Dockerfile builds alike, since
// neither sees .git), else "dev". `go run` stamps nothing, so it shows "dev".
// A build with uncommitted changes is not marked "-dirty": the buildpack's build always counts as modified, so the mark said nothing.
var buildCommit = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	if c := os.Getenv("COMMIT_HASH"); c != "" {
		return c
	}
	return "dev"
}()

// shortCommit is buildCommit cut to the usual seven characters, without any "-dirty" suffix.
func shortCommit(c string) string {
	c = strings.TrimSuffix(c, "-dirty")
	if len(c) > 7 {
		c = c[:7]
	}
	return c
}
