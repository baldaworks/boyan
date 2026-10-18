package canonicalsqltest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed coverage.json
var registeredCoverage []byte

// CoverageEntry assigns a current preservation surface to its SQL migration task.
type CoverageEntry struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Symbol string `json:"symbol"`
	Detail string `json:"detail,omitempty"`
	Owner  string `json:"owner"`
}

// DiscoverCoverage inventories current Badger ownership and consumer tests.
//
//nolint:cyclop // The fixed inventory explicitly selects declarations and consumer test files.
func DiscoverCoverage(source fs.FS) ([]CoverageEntry, error) {
	files, imports, err := coverageSources(source)
	if err != nil {
		return nil, err
	}

	consumers := coverageConsumers(imports)

	var entries []CoverageEntry

	for name, file := range files {
		isStore := path.Dir(name) == "store/badger"

		isTest := strings.HasSuffix(name, "_test.go")
		if !isStore && !(isTest && consumers[path.Dir(name)]) {
			continue
		}

		for _, declaration := range file.Decls {
			if general, ok := declaration.(*ast.GenDecl); ok && isStore && !isTest && general.Tok == token.CONST {
				entries = append(entries, coverageFamilies(name, general)...)
			}

			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				entries = append(entries, coverageFunction(name, function, isTest)...)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}

		if a.File != b.File {
			return a.File < b.File
		}

		return a.Symbol < b.Symbol
	})

	return entries, nil
}

//nolint:cyclop // One filesystem walk validates source files and collects imports.
func coverageSources(source fs.FS) (map[string]*ast.File, map[string][]string, error) {
	files := make(map[string]*ast.File)
	imports := make(map[string][]string)
	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if name != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "testdata") {
				return fs.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			return fmt.Errorf("parse coverage source %s: %w", name, err)
		}

		files[name] = file
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}

			pkg := path.Dir(name)
			imports[pkg] = append(imports[pkg], strings.TrimPrefix(importPath, "github.com/baldaworks/boyan/"))
		}

		return nil
	})

	return files, imports, err
}

func coverageConsumers(imports map[string][]string) map[string]bool {
	// Include complete consumer packages, so a shared helper in another file
	// cannot hide an indirectly constructed backend from the inventory.
	consumers := map[string]bool{"store/badger": true}

	for changed := true; changed; {
		changed = false

		for pkg, dependencies := range imports {
			if consumers[pkg] {
				continue
			}

			for _, dependency := range dependencies {
				if consumers[dependency] {
					consumers[pkg] = true
					changed = true

					break
				}
			}
		}
	}

	return consumers
}

func coverageFamilies(name string, declaration *ast.GenDecl) []CoverageEntry {
	var entries []CoverageEntry

	for _, spec := range declaration.Specs {
		values, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		for i, identifier := range values.Names {
			if !strings.HasPrefix(identifier.Name, "badgerRecord") || i >= len(values.Values) {
				continue
			}

			literal, ok := values.Values[i].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}

			family, _ := strconv.Unquote(literal.Value) // ParseFile validated the string literal.
			entries = append(entries, CoverageEntry{
				Kind: "family", File: name, Symbol: identifier.Name, Detail: family, Owner: familyOwner(family),
			})
		}
	}

	return entries
}

//nolint:cyclop // Each fixed preservation category is registered explicitly.
func coverageFunction(name string, function *ast.FuncDecl, isTest bool) []CoverageEntry {
	symbol := function.Name.Name
	if function.Recv != nil {
		symbol = expressionName(function.Recv.List[0].Type) + "." + symbol
	}

	owner := coverageOwner(name, symbol)

	var entries []CoverageEntry

	add := func(kind, detail, task string) {
		entries = append(entries, CoverageEntry{Kind: kind, File: name, Symbol: symbol, Detail: detail, Owner: task})
	}
	writes, keys, raw := coverageCalls(function)

	if isTest {
		kind := "fixture"

		for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
			if strings.HasPrefix(function.Name.Name, prefix) {
				kind = "case"

				break
			}
		}

		add(kind, "", owner)

		if len(raw) != 0 {
			add("raw-fixture", uniqueNames(raw), owner)
		}

		return entries
	}

	lower := strings.ToLower(function.Name.Name)

	namedKey := containsAny(lower, "key", "prefix") && returnsBytes(function)
	if len(keys) != 0 || namedKey || function.Name.Name == "payloadScope" {
		add("key", uniqueNames(keys), "TASK-002")
	}

	if len(writes) != 0 {
		add("writer", uniqueNames(writes), owner)
	}

	return entries
}

//nolint:cyclop // Recognizes the existing raw call and key forms without a type-analysis framework.
func coverageCalls(function *ast.FuncDecl) (writes, keys, raw []string) {
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			member := selector.Sel.Name
			if member == "db" || member == "DB" || strings.HasPrefix(member, "before") || strings.HasPrefix(member, "after") {
				raw = append(raw, expressionName(selector))
			}
		}

		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		callee := expressionName(call.Fun)
		parts := strings.Split(callee, ".")

		short := parts[len(parts)-1]
		if short == "TupleKey" {
			keys = append(keys, callee)
		}

		if isWriteCall(short) && (short != "Load" || callee == "db.Load" || strings.HasSuffix(callee, ".db.Load")) {
			writes = append(writes, callee)
			raw = append(raw, callee)
		}

		if strings.HasPrefix(callee, "badger.") {
			raw = append(raw, callee)
		}

		return true
	})

	return writes, keys, raw
}

// RegisteredCoverage reads the checked preservation inventory.
func RegisteredCoverage() ([]CoverageEntry, error) {
	var entries []CoverageEntry

	err := json.Unmarshal(registeredCoverage, &entries)

	return entries, err
}

func expressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return expressionName(value.X) + "." + value.Sel.Name
	case *ast.StarExpr:
		return "*" + expressionName(value.X)
	case *ast.IndexExpr:
		return expressionName(value.X)
	default:
		return ""
	}
}

func returnsBytes(function *ast.FuncDecl) bool {
	if function.Type.Results == nil {
		return false
	}

	for _, result := range function.Type.Results.List {
		if array, ok := result.Type.(*ast.ArrayType); ok && array.Len == nil && expressionName(array.Elt) == "byte" {
			return true
		}
	}

	return false
}

func uniqueNames(names []string) string {
	sort.Strings(names)

	var unique []string
	for _, name := range names {
		if len(unique) == 0 || unique[len(unique)-1] != name {
			unique = append(unique, name)
		}
	}

	return strings.Join(unique, ",")
}

func isWriteCall(name string) bool {
	switch name {
	case "Set", "SetEntry", "Delete", "Load", "Update", "NewWriteBatch", "DropAll", "DropPrefix",
		"Write", "WriteString", "WriteFile", "Rename", "Remove", "Sync":
		return true
	}

	for _, prefix := range []string{"putCanonical", "putBadger", "putFenced"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

func familyOwner(family string) string {
	switch {
	case strings.HasPrefix(family, "source-privacy"):
		return "TASK-005"
	case containsAny(family, "certificate", "capability"):
		return "TASK-006"
	case containsAny(family, "erase", "denied", "forget-outcome"):
		return "TASK-012"
	case strings.HasPrefix(family, "ingest"):
		return "TASK-010"
	case containsAny(family, "delivery", "projection"):
		return "TASK-011"
	default:
		return "TASK-007"
	}
}

//nolint:cyclop // Keep the approved migration task routing explicit and reviewable.
func coverageOwner(file, symbol string) string {
	name := strings.ToLower(file + " " + symbol)
	switch {
	case strings.HasPrefix(file, "index/sqlite/"):
		return "TASK-018"
	case containsAny(file, "standaloneruntime/", "pkg/boyan/"):
		return "TASK-017"
	case strings.Contains(file, "orderedingest/"):
		return "TASK-010"
	case !strings.HasPrefix(file, "store/badger/"):
		return "TASK-019"
	case strings.Contains(name, "logical"):
		return "TASK-015"
	case containsAny(name, "restore", "native_privacy_merge", "loadnativeprivacybackup"):
		return "TASK-014"
	case containsAny(name, "erase", "denied", "denial", "forget"):
		return "TASK-012"
	case containsAny(name, "trace", "boundary_view", "boundary_resolver"):
		return "TASK-009"
	case containsAny(name, "certificate", "capability", "identity", "proof"):
		return "TASK-006"
	case strings.Contains(name, "privacy") && !strings.Contains(name, "native_privacy_stream"):
		return "TASK-005"
	case containsAny(name, "backup", "native_privacy_stream", "integrity"):
		return "TASK-013"
	case strings.Contains(name, "ingest_sequence"):
		return "TASK-010"
	case containsAny(name, "projection", "checkpoint", "delivery"):
		return "TASK-011"
	case containsAny(name, "reader", "active_reads"):
		return "TASK-008"
	default:
		return "TASK-007"
	}
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}

	return false
}
