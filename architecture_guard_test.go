package boyan_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const (
	modulePath      = "github.com/baldaworks/boyan"
	internalAppPath = "github.com/baldaworks/boyan/internal/apps/boyan"
	removedAppPath  = "github.com/baldaworks/boyan/app"
)

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func TestArchitectureGuard(t *testing.T) {
	packages := listRepositoryPackages(t)

	root, ok := packages[modulePath]
	if !ok {
		t.Fatalf("root package %q not found", modulePath)
	}
	for _, imported := range root.Imports {
		if !isStdlibImport(imported) {
			t.Fatalf("root package imports non-stdlib dependency %q", imported)
		}
	}

	if _, ok := packages[removedAppPath]; ok {
		t.Fatalf("removed public package %q is still present", removedAppPath)
	}

	allowedInternalAppImporters := map[string]struct{}{
		modulePath + "/cmd/boyan":           {},
		modulePath + "/internal/derivation": {},
		modulePath + "/internal/httpingest": {},
	}

	cmdImportsInternalApp := false
	for importPath, pkg := range packages {
		for _, imported := range pkg.Imports {
			if imported != removedAppPath && strings.Contains(imported, removedAppPath) {
				t.Fatalf("package %q still imports removed app path %q", importPath, imported)
			}
			if imported != internalAppPath {
				continue
			}
			if importPath == modulePath+"/cmd/boyan" {
				cmdImportsInternalApp = true
			}
			if _, ok := allowedInternalAppImporters[importPath]; !ok {
				t.Fatalf("package %q must not import internal app layer %q", importPath, internalAppPath)
			}
		}
	}
	if !cmdImportsInternalApp {
		t.Fatalf("composition root %q does not import %q", modulePath+"/cmd/boyan", internalAppPath)
	}
}

func listRepositoryPackages(t *testing.T) map[string]listedPackage {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -json ./... failed: %v\n%s", err, output)
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	packages := make(map[string]listedPackage)
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("decode go list output: %v", err)
		}
		packages[pkg.ImportPath] = pkg
	}
	return packages
}

func isStdlibImport(path string) bool {
	first := path
	if index := strings.IndexByte(first, '/'); index >= 0 {
		first = first[:index]
	}
	return !strings.Contains(first, ".")
}
