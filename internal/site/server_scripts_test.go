package site

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScriptsParse syntax-checks the embedded browser scripts. A single stray quote stops a whole script from
// loading, and then its buttons silently do nothing, which no Go test would notice. It needs node, so it is skipped
// where node isn't installed.
func TestScriptsParse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	files, err := filepath.Glob("static/*.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("no scripts found: %v", err)
	}
	for _, f := range files {
		if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
			t.Errorf("%s does not parse:\n%s", f, out)
		}
	}
}
