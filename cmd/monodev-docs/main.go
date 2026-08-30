// Command monodev-docs generates release-time documentation for monodev.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/cli"
	"github.com/spf13/cobra/doc"
)

func main() {
	outputDir := flag.String("output", "dist", "directory for generated release artifacts")
	version := flag.String("version", "dev", "version embedded in generated documentation")
	flag.Parse()

	if err := generate(*outputDir, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(outputDir, version string) error {
	if outputDir == "" {
		return fmt.Errorf("output directory must not be empty")
	}
	if version == "" {
		return fmt.Errorf("version must not be empty")
	}

	cli.SetVersion(version)
	root := cli.RootCommand()
	completionDir := filepath.Join(outputDir, "completions")
	manDir := filepath.Join(outputDir, "man")
	for _, dir := range []string{completionDir, manDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	completionGenerators := []struct {
		name string
		fn   func(io.Writer) error
	}{
		{name: "monodev.bash", fn: root.GenBashCompletion},
		{name: "_monodev", fn: root.GenZshCompletion},
		{name: "monodev.fish", fn: func(file io.Writer) error {
			return root.GenFishCompletion(file, true)
		}},
	}
	for _, generator := range completionGenerators {
		path := filepath.Join(completionDir, generator.name)
		file, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		if err := generator.fn(file); err != nil {
			_ = file.Close()
			return fmt.Errorf("generate %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", path, err)
		}
	}

	header := &doc.GenManHeader{
		Title:   "MONODEV",
		Section: "1",
		Source:  fmt.Sprintf("monodev %s", version),
		Manual:  "Monodev Manual",
	}
	if err := doc.GenManTree(root, header, manDir); err != nil {
		return fmt.Errorf("generate man pages: %w", err)
	}

	return nil
}
