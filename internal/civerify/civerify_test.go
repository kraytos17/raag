// Package civverify keeps GitHub Actions workflow files valid YAML and pins
// action references to resolvable tags (no @master / @latest mutable refs).
package civverify

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// mutableRefRE matches a workflow `uses:` line referencing a mutable tag.
var (
	mutableRefRE = regexp.MustCompile(`uses:\s+\S+@(master|latest|main|\${{)`)
	usesRE       = regexp.MustCompile(`(?m)^\s*uses:\s+(\S+)$`)
)

func workflowFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		t.Fatal("no workflow files found")
	}
	return files
}

func TestWorkflows_ValidYAML(t *testing.T) {
	for _, f := range workflowFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			t.Errorf("%s: invalid YAML: %v", f, err)
		}
	}
}

func TestWorkflows_NoMutableActionRefs(t *testing.T) {
	for _, f := range workflowFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range usesRE.FindAllString(string(data), -1) {
			if mutableRefRE.MatchString(m) {
				t.Errorf("%s: mutable action ref (pin a release tag): %s", f, strings.TrimSpace(m))
			}
		}
	}
}
