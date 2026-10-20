package site

import (
	"os"
	"runtime/debug"
)

// buildCommit is the git commit the server was built from: the revision Go stamps into
// `go build` binaries when .git is present, else $COMMIT_HASH (set it on DigitalOcean App
// Platform to ${_self.COMMIT_HASH}, which covers buildpack and Dockerfile builds alike, since
// neither sees .git), else "dev". `go run` stamps nothing, so it shows "dev".
var buildCommit = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		var rev, dirty string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if rev != "" {
			return rev + dirty
		}
	}
	if c := os.Getenv("COMMIT_HASH"); c != "" {
		return c
	}
	return "dev"
}()

// shortCommit is buildCommit cut to the usual seven characters (keeping a "-dirty" suffix).
func shortCommit(c string) string {
	rev, suffix := c, ""
	if len(c) > 6 && c[len(c)-6:] == "-dirty" {
		rev, suffix = c[:len(c)-6], "-dirty"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return rev + suffix
}
