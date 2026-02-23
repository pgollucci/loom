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
	cmd.AddCommand(newProjectDashboardCommand())

	return cmd
}

func newProjectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Listing all projects...")
		},
	}
}

func newProjectShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <project_id>",
		Short: "Show details of a project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Showing details for project: %s\n", args[0])
		},
	}
}

func newProjectDashboardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard <project_id>",
		Short: "Show dashboard for a project",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Showing dashboard for project: %s\n", args[0])
		},
	}
}
