package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	cmd.AddCommand(newProjectListCommand())
	cmd.AddCommand(newProjectShowCommand())
	cmd.AddCommand(newProjectBeadListCommand())

	return cmd
}

func newProjectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Listing all projects...")
			return nil
		},
	}
}

func newProjectShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <project_id>",
		Short: "Show project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectID := args[0]
			fmt.Printf("Showing details for project: %s\n", projectID)
			return nil
		},
	}
}

func newProjectBeadListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "<project_id> bead list",
		Short: "List beads for a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectID := args[0]
			fmt.Printf("Listing beads for project: %s\n", projectID)
			return nil
		},
	}
}
