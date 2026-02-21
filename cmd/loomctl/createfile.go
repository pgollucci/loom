package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
)

func newCreateFileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "createfile",
		Short: "Create a file called AUTONOMY_TEST.md with a single line: Loom is autonomous",
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := "AUTONOMY_TEST.md"
			content := "Loom is autonomous\n"

			file, err := os.Create(filePath)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			defer file.Close()

			_, err = file.WriteString(content)
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}

			log.Printf("Created file %s with content: %s", filePath, content)
			return nil
		},
	}
	return cmd
}
