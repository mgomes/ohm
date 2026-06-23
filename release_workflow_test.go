package ohm

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestReleaseWorkflowPinsWritePrivilegedActions(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatalf("os.ReadFile(release workflow) error = %v, want nil", err)
	}

	workflow := string(data)
	for _, action := range []string{
		"actions/checkout",
		"actions/setup-go",
		"goreleaser/goreleaser-action",
	} {
		pattern := regexp.MustCompile(`uses:\s+` + regexp.QuoteMeta(action) + `@[0-9a-f]{40}\b`)
		if !pattern.MatchString(workflow) {
			t.Errorf("release workflow action %s is not pinned to a full commit SHA", action)
		}
		if strings.Contains(workflow, "uses: "+action+"@v") {
			t.Errorf("release workflow action %s still uses a mutable version tag", action)
		}
	}
}
