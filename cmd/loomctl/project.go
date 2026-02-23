package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("project ID required")
			}
			projectID := args[0]
			fmt.Printf("Showing dashboard for project: %s\n", projectID)
			// Here you would add logic to display the project's dashboard
			return nil
		},
	}

	cmd.AddCommand(newBeadCommand())
	cmd.AddCommand(newWorkflowCommand())
	cmd.AddCommand(newAgentCommand())
	// Add more subcommands as needed

	return cmd
}
