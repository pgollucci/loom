package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newContainerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container",
		Short: "Manage containers",
	}
	cmd.AddCommand(newContainerListCommand())
	cmd.AddCommand(newContainerLogsCommand())
	cmd.AddCommand(newContainerRestartCommand())
	cmd.AddCommand(newContainerStatusCommand())
	return cmd
}

func newContainerListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all project containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Listing containers...")
			client := newClient()
			params := url.Values{}
			params.Set("project", args[0])
			resp, err := client.get("/containers", params)
			if err != nil {
				return fmt.Errorf("failed to list containers: %w", err)
			}
			outputJSON(resp)

			return nil
		},
	}
}

func newContainerLogsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <project>",
		Short: "Tail container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Tailing logs for project: %s\n", args[0])
			client := newClient()
			params := url.Values{}
			params.Set("project", args[0])
			resp, err := client.get(fmt.Sprintf("/containers/%s/logs", args[0]), nil)
			if err != nil {
				return fmt.Errorf("failed to get logs: %w", err)
			}
			outputJSON(resp)

			return nil
		},
	}
}

func newContainerRestartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <project>",
		Short: "Restart project container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Restarting container for project: %s\n", args[0])
			client := newClient()
			params := url.Values{}
			params.Set("project", args[0])
			resp, err := client.post(fmt.Sprintf("/containers/%s/restart", args[0]), nil)
			if err != nil {
				return fmt.Errorf("failed to restart container: %w", err)
			}
			outputJSON(resp)

			return nil
		},
	}
}

func newContainerStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <project>",
		Short: "Get container status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Getting status for project: %s\n", args[0])
			client := newClient()
			params := url.Values{}
			params.Set("project", args[0])
			resp, err := client.get(fmt.Sprintf("/containers/%s/status", args[0]), nil)
			if err != nil {
				return fmt.Errorf("failed to get container status: %w", err)
			}
			outputJSON(resp)

			return nil
		},
	}
}
