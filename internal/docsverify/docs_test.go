// Package docsverify keeps the README's documented CLI surface in sync with
// the real `raag` binary. It builds the CLI once, walks every documented
// `./bin/raag <cmd> [<subcmd>]` path from README.md, and asserts each command
// (and two-level subcommand) exists in the binary's --help output.
package docsverify

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cmdPathRE matches `./bin/raag <word>` (and optional nested `<word>`) inside
// fenced code blocks, so prose sentences ("raag and friends") are ignored.
var cmdPathRE = regexp.MustCompile(`(?m)^\s*\./bin/raag ([a-z]+)(?: ([a-z]+))?$`)

// buildCLI compiles ./cmd/raag into a temp dir and returns its path.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "raag")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/raag")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/raag: %v\n%s", err, out)
	}
	return bin
}

// repoRoot returns the repository root (parent of internal/docsverify).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd))
}

// helpFor runs `bin <args...> --help` and returns stdout+stderr.
func TestREADMECommandsExistInCLI(t *testing.T) {
	bin := buildCLI(t)
	readme, err := os.ReadFile(filepath.Join(repoRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	var missing []string
	for _, m := range cmdPathRE.FindAllStringSubmatch(string(readme), -1) {
		path := []string{m[1]}
		if m[2] != "" {
			path = append(path, m[2])
		}

		key := strings.Join(path, " ")
		if seen[key] {
			continue
		}

		seen[key] = true
		if !commandExists(bin, path) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("README documents commands that do not exist in the CLI:\n  %s\n(update README.md or add the command)", strings.Join(missing, "\n  "))
	}
}

// commandExists walks the command tree for the given path by checking that
// each level's --help names the next-level subcommand.
func commandExists(bin string, path []string) bool {
	// Top-level: the binary must accept the first subcommand.
	help, ok := helpQuiet(bin)
	if !ok {
		return false
	}
	if !mentionsCommand(help, path[0]) {
		return false
	}
	// Nested: `<parent> --help` must list `<child>`.
	if len(path) == 2 {
		parentHelp, ok := helpQuiet(bin, path[0])
		if !ok || !mentionsCommand(parentHelp, path[1]) {
			return false
		}
	}
	return true
}

// helpQuiet runs `bin <args...> --help` and returns combined output plus
// whether the invocation succeeded.
func helpQuiet(bin string, args ...string) (string, bool) {
	cmdArgs := append(append([]string{}, args...), "--help")
	cmd := exec.Command(bin, cmdArgs...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), false
	}
	return out.String(), true
}

// mentionsCommand reports whether help output lists a command named `name`
// (matching cobra's "Available Commands" section entries).
func mentionsCommand(help, name string) bool {
	for line := range strings.SplitSeq(help, "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}
