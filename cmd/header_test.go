package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/parser"
)

// Every static report opens with the project it's for, named as global
// names it. summary -d names the storage directory only with -v.
func TestE2EReportsNameTheirProject(t *testing.T) {
	root := setupE2EFixture(t)
	projects, err := parser.DiscoverAllProjects()
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for _, p := range projects {
		if p.EncodedPath == e2eProjDir {
			want = p.DisplayName
		}
	}
	if want == "" {
		t.Fatalf("fixture project %s not discovered", e2eProjDir)
	}
	if got := parser.ProjectDisplayName(filepath.Join(root, "projects", e2eProjDir)); got != want {
		t.Errorf("ProjectDisplayName = %q, DiscoverAllProjects says %q", got, want)
	}

	for _, args := range [][]string{
		{"show", projFlag, e2eAlphaID},
		{"list", projFlag},
		{"summary", projFlag},
		{"summary", projFlag, "-d"},
	} {
		out, _, err := executeCLISplit(t, append(args, "--no-color")...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		header := strings.SplitN(out, "\n", 3)[1]
		if !strings.Contains(header, "  "+want+"  ") {
			t.Errorf("%v: header should name %q:\n%s", args, want, header)
		}
		if strings.Contains(out, "Storage:") || strings.Contains(out, "Project:") {
			t.Errorf("%v: storage directory without -v:\n%s", args, out)
		}
	}

	out, _, err := executeCLISplit(t, "summary", projFlag, "-d", "-v", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Storage: "+filepath.Join(root, "projects", e2eProjDir)) {
		t.Errorf("summary -d -v should name the storage directory:\n%s", out)
	}
}
