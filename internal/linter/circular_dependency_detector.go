package linter

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// DetectCircularDependencies checks for circular dependencies in Go packages
func DetectCircularDependencies(root string) error {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("failed to parse directory: %w", err)
	}

	// Map to track package imports
	importsMap := make(map[string][]string)

	// Populate imports map
	for pkgName, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				importPath := imp.Path.Value
				importsMap[pkgName] = append(importsMap[pkgName], importPath)
			}
		}
	}

	// Check for cycles
	visited := make(map[string]bool)
	stack := make(map[string]bool)

	var visit func(string) bool
	visit = func(pkg string) bool {
		if stack[pkg] {
			return true // cycle detected
		}
		if visited[pkg] {
			return false
		}
		visited[pkg] = true
		stack[pkg] = true
		for _, imp := range importsMap[pkg] {
			if visit(imp) {
				return true
			}
		}
		stack[pkg] = false
		return false
	}

	for pkg := range importsMap {
		if visit(pkg) {
			return fmt.Errorf("circular dependency detected in package: %s", pkg)
		}
	}

	return nil
}

func main() {
	root := "."
	err := DetectCircularDependencies(root)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	fmt.Println("No circular dependencies detected.")
}
