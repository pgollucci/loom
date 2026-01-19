package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var skipDirs = map[string]struct{}{
	".git":         {},
	"bin":          {},
	"obj":          {},
	"node_modules": {},
	"dist":         {},
	"vendor":       {},
}

func main() {
	errors := make([]error, 0)
	root := "."

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errors = append(errors, err)
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if _, ok := skipDirs[name]; ok {
				return fs.SkipDir
			}
			return nil
		}

		if !isYAML(path) {
			return nil
		}
		if err := validateYAMLFile(path); err != nil {
			errors = append(errors, err)
		}
		return nil
	})

	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func isYAML(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}

func validateYAMLFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer file.Close()

	dec := yaml.NewDecoder(file)
	for {
		var payload interface{}
		if err := dec.Decode(&payload); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("%s: %w", path, err)
		}
	}
}
