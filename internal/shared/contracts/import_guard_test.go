package contracts

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImportGuard_ProtoImportsStayInSharedBoundary(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	forbiddenRoots := []string{
		filepath.Join(repoRoot, "internal/core"),
		filepath.Join(repoRoot, "internal/actors"),
		filepath.Join(repoRoot, "internal/interfaces"),
	}

	var violations []string
	for _, root := range forbiddenRoots {
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("stat %s: %v", root, err)
		}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == "vendor" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}

			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return fmt.Errorf("parse imports for %s: %w", path, err)
			}
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, "\"")
				if !isForbiddenProtoImport(importPath) {
					continue
				}
				rel, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					return relErr
				}
				violations = append(violations, fmt.Sprintf("%s imports %s", rel, importPath))
			}
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(violations) == 0 {
		return
	}
	slices.Sort(violations)
	t.Fatalf("protobuf imports are restricted to internal/shared/* and proto/gen\nviolations:\n%s", strings.Join(violations, "\n"))
}

func TestImportGuard_DetectsSimulatedViolationFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "import_guard", "violating_import.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports for fixture %s: %v", path, err)
	}

	found := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		if isForbiddenProtoImport(importPath) {
			found = append(found, importPath)
		}
	}
	slices.Sort(found)
	want := []string{
		"github.com/golang/protobuf/proto",
		"google.golang.org/protobuf/proto",
	}
	if !slices.Equal(found, want) {
		t.Fatalf("forbidden imports=%v want=%v", found, want)
	}
}

func isForbiddenProtoImport(importPath string) bool {
	importPath = strings.TrimSpace(importPath)
	return strings.HasPrefix(importPath, "google.golang.org/protobuf") ||
		strings.HasPrefix(importPath, "github.com/golang/protobuf")
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("unable to locate repository root from %s", wd)
		}
		dir = parent
	}
}
