package voicegoio_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// SPEC.md 1 and 9: go.mod has zero requires and nothing outside the standard
// library is reachable. This is a hard constraint, not a preference: the
// library ships inside a flight simulator add-on that must work with the
// network adapter disabled, and every dependency is a future supply chain
// problem for a program that has no business talking to anything.
//
// CI enforces the same rule, but having it as a test means it fails on the
// developer's machine at the moment the dependency is added.
func TestNoModuleRequirements(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require") {
			t.Errorf("go.mod:%d has a require directive: %s", i+1, trimmed)
		}
	}
}

// go list is the real check: a package can reach a non stdlib dependency
// through a transitive import even when go.mod looks clean.
func TestOnlyStandardLibraryIsImported(t *testing.T) {
	if testing.Short() {
		t.Skip("go list is slow")
	}
	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	const self = "github.com/mrlm-net/voice-goio"
	for _, pkg := range strings.Fields(string(out)) {
		if strings.HasPrefix(pkg, self) {
			continue
		}
		// Standard library import paths have no dot in their first element.
		first, _, _ := strings.Cut(pkg, "/")
		if strings.Contains(first, ".") {
			t.Errorf("non-stdlib dependency: %s", pkg)
		}
	}
}
