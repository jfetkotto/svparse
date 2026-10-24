package svparse_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The module lives at the /v2 import path because the work since v0.1.9 broke
// the v0 API (Go modules, "Go Modules: v2 and Beyond"). This test keeps the
// path and every import of it from drifting back to the unversioned one.
func TestModulePathIsV2(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(mod), "module github.com/jfetkotto/svparse/v2\n") {
		t.Fatalf("go.mod must declare module github.com/jfetkotto/svparse/v2, got %q", strings.SplitN(string(mod), "\n", 2)[0])
	}
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "module_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range []string{`"github.com/jfetkotto/svparse"`, `"github.com/jfetkotto/svparse/`} {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, bad) && !strings.Contains(line, `"github.com/jfetkotto/svparse/v2`) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
					t.Errorf("%s imports the unversioned module path: %s", path, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
