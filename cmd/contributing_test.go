package cmd

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestCONTRIBUTINGPinsLintVersions fails when the lint command CONTRIBUTING
// documents for running without make drifts from what `make lint` runs. The
// command spells the versions out rather than reading go.mod, because the
// section exists for Windows, and PowerShell and cmd would each need their
// own way to read it. So a toolchain bump, Dependabot's included, has to
// update CONTRIBUTING in the same PR.
func TestCONTRIBUTINGPinsLintVersions(t *testing.T) {
	data, err := os.ReadFile("../CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("read CONTRIBUTING: %v", err)
	}
	doc := string(data)
	toolchain := lintToolchain(t)
	linter := makeVariable(t, "GOLANGCI_LINT_VERSION")

	// One message per stale mention, with its line, which is what the fix
	// needs. The whole command is checked only when every version in it is
	// current, where a miss means its shape changed.
	goVersion := regexp.MustCompile(`go1\.\d+(\.\d+)?`)
	linterVersion := regexp.MustCompile(`golangci-lint(?: |@)(v\d+\.\d+\.\d+)`)
	stale := false
	for i, line := range strings.Split(doc, "\n") {
		for _, m := range goVersion.FindAllString(line, -1) {
			if m != toolchain {
				stale = true
				t.Errorf("CONTRIBUTING.md:%d says %s, but go.mod's toolchain is %s", i+1, m, toolchain)
				break
			}
		}
		for _, m := range linterVersion.FindAllStringSubmatch(line, -1) {
			if m[1] != linter {
				stale = true
				t.Errorf("CONTRIBUTING.md:%d says golangci-lint %s, but the Makefile's GOLANGCI_LINT_VERSION is %s", i+1, m[1], linter)
				break
			}
		}
	}
	cmd := "GOTOOLCHAIN=" + toolchain + " go run github.com/golangci/golangci-lint/cmd/golangci-lint@" + linter + " run ./..."
	if !stale && !strings.Contains(doc, cmd) {
		t.Errorf("CONTRIBUTING.md no longer gives the lint command make runs:\n  %s", cmd)
	}
}

// lintToolchain reads go.mod the way the Makefile's LINT_GOTOOLCHAIN does:
// the toolchain line, or "go" and the go line's version when there is none.
func lintToolchain(t *testing.T) string {
	t.Helper()
	f, err := os.Open("../go.mod")
	if err != nil {
		t.Fatalf("open go.mod: %v", err)
	}
	defer func() { _ = f.Close() }()
	var goLine, toolchain string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "go":
			goLine = "go" + fields[1]
		case "toolchain":
			toolchain = fields[1]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if toolchain != "" {
		return toolchain
	}
	if goLine == "" {
		t.Fatal("go.mod has neither a toolchain line nor a go line")
	}
	return goLine
}

// makeVariable returns the value of a NAME=value line in the Makefile.
func makeVariable(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), name+"="); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("Makefile sets no %s", name)
	return ""
}
