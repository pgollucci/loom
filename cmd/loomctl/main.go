package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const version = "1.0.0"

var (
	serverURL string
	outputFormat string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "loomctl",
		Short: "Loom CLI - interact with your Loom server",
		Long: `loomctl is a command-line interface for interacting with Loom servers.
It provides intuitive commands for managing beads, workflows, agents, and projects.`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", getDefaultServer(), "Loom server URL")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")

	// Add subcommands
	rootCmd.AddCommand(newBeadCommand())
	rootCmd.AddCommand(newWorkflowCommand())
	rootCmd.AddCommand(newAgentCommand())
	rootCmd.AddCommand(newProjectCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getDefaultServer() string {
	if server := os.Getenv("LOOM_SERVER"); server != "" {
		return server
	}
	return "http://localhost:8080"
}

// HTTP client helper
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func newClient() *Client {
	return &Client{
		BaseURL: serverURL,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.BaseURL, path)
	if params != nil {
		u += "?" + params.Encode()
	}

	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) post(path string, data interface{}) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.BaseURL, path)

	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data: %w", err)
		}
		body = strings.NewReader(string(jsonData))
	}

	req, err := http.NewRequest("POST", u, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Bead commands
func newBeadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bead",
		Short: "Manage beads",
		Long:  "Create, list, and manage beads (work items) in Loom",
	}

	cmd.AddCommand(newBeadListCommand())
	cmd.AddCommand(newBeadCreateCommand())
	cmd.AddCommand(newBeadShowCommand())
	cmd.AddCommand(newBeadClaimCommand())

	return cmd
}

func newBeadListCommand() *cobra.Command {
	var (
		projectID  string
		status     string
		beadType   string
		assignedTo string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List beads",
		Long:  "List beads with optional filters",
		Example: `  loomctl bead list
  loomctl bead list --project=loom-self
  loomctl bead list --status=open
  loomctl bead list --assigned-to=agent-123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			params := url.Values{}
			if projectID != "" {
				params.Set("project_id", projectID)
			}
			if status != "" {
				params.Set("status", status)
			}
			if beadType != "" {
				params.Set("type", beadType)
			}
			if assignedTo != "" {
				params.Set("assigned_to", assignedTo)
			}

			data, err := client.get("/api/v1/beads", params)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var beads []map[string]interface{}
			if err := json.Unmarshal(data, &beads); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(beads) == 0 {
				fmt.Println("No beads found")
				return nil
			}

			// Table output
			fmt.Printf("%-12s %-50s %-15s %-10s\n", "ID", "TITLE", "STATUS", "PRIORITY")
			fmt.Println(strings.Repeat("-", 90))
			for _, bead := range beads {
				id := getString(bead, "id")
				title := getString(bead, "title")
				if len(title) > 47 {
					title = title[:47] + "..."
				}
				status := getString(bead, "status")
				priority := fmt.Sprintf("P%v", bead["priority"])
				fmt.Printf("%-12s %-50s %-15s %-10s\n", id, title, status, priority)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Filter by project ID")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (open, in_progress, completed)")
	cmd.Flags().StringVar(&beadType, "type", "", "Filter by bead type")
	cmd.Flags().StringVar(&assignedTo, "assigned-to", "", "Filter by assigned agent")

	return cmd
}

func newBeadCreateCommand() *cobra.Command {
	var (
		title       string
		description string
		priority    int
		projectID   string
		beadType    string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new bead",
		Long:  "Create a new bead (work item) in Loom",
		Example: `  loomctl bead create --title="Fix bug" --project=loom-self
  loomctl bead create --title="Add feature" --description="Detailed description" --priority=0 --project=loom-self`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if title == "" || projectID == "" {
				return fmt.Errorf("--title and --project are required")
			}

			client := newClient()
			data := map[string]interface{}{
				"title":       title,
				"description": description,
				"priority":    priority,
				"project_id":  projectID,
			}
			if beadType != "" {
				data["type"] = beadType
			}

			respData, err := client.post("/api/v1/beads", data)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(respData))
				return nil
			}

			var bead map[string]interface{}
			if err := json.Unmarshal(respData, &bead); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("✅ Bead created successfully!\n")
			fmt.Printf("ID: %s\n", getString(bead, "id"))
			fmt.Printf("Title: %s\n", getString(bead, "title"))
			fmt.Printf("Status: %s\n", getString(bead, "status"))
			fmt.Printf("Priority: P%v\n", bead["priority"])

			return nil
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "Bead title (required)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Bead description")
	cmd.Flags().IntVar(&priority, "priority", 2, "Priority (0=highest, 4=lowest)")
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID (required)")
	cmd.Flags().StringVar(&beadType, "type", "task", "Bead type")

	cmd.MarkFlagRequired("title")
	cmd.MarkFlagRequired("project")

	return cmd
}

func newBeadShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <bead-id>",
		Short: "Show bead details",
		Long:  "Display detailed information about a specific bead",
		Args:  cobra.ExactArgs(1),
		Example: `  loomctl bead show loom-001`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/beads/%s", args[0]), nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var bead map[string]interface{}
			if err := json.Unmarshal(data, &bead); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("ID:          %s\n", getString(bead, "id"))
			fmt.Printf("Title:       %s\n", getString(bead, "title"))
			fmt.Printf("Type:        %s\n", getString(bead, "type"))
			fmt.Printf("Status:      %s\n", getString(bead, "status"))
			fmt.Printf("Priority:    P%v\n", bead["priority"])
			fmt.Printf("Project:     %s\n", getString(bead, "project_id"))
			fmt.Printf("Assigned To: %s\n", getString(bead, "assigned_to"))
			fmt.Printf("Created:     %s\n", getString(bead, "created_at"))
			fmt.Printf("\nDescription:\n%s\n", getString(bead, "description"))

			return nil
		},
	}

	return cmd
}

func newBeadClaimCommand() *cobra.Command {
	var agentID string

	cmd := &cobra.Command{
		Use:   "claim <bead-id>",
		Short: "Claim a bead",
		Long:  "Assign a bead to an agent",
		Args:  cobra.ExactArgs(1),
		Example: `  loomctl bead claim loom-001 --agent=agent-123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentID == "" {
				return fmt.Errorf("--agent is required")
			}

			client := newClient()
			data := map[string]interface{}{
				"agent_id": agentID,
			}

			respData, err := client.post(fmt.Sprintf("/api/v1/beads/%s/claim", args[0]), data)
			if err != nil {
				return err
			}

			fmt.Printf("✅ Bead %s claimed by agent %s\n", args[0], agentID)

			if outputFormat == "json" {
				fmt.Println(string(respData))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentID, "agent", "a", "", "Agent ID (required)")
	cmd.MarkFlagRequired("agent")

	return cmd
}

// Workflow commands
func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
		Long:  "List and start workflows in Loom",
	}

	cmd.AddCommand(newWorkflowListCommand())
	cmd.AddCommand(newWorkflowShowCommand())
	cmd.AddCommand(newWorkflowStartCommand())

	return cmd
}

func newWorkflowListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows",
		Long:  "List all available workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/workflows", nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var response map[string]interface{}
			if err := json.Unmarshal(data, &response); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			workflows, ok := response["workflows"].([]interface{})
			if !ok || len(workflows) == 0 {
				fmt.Println("No workflows found")
				return nil
			}

			// Table output
			fmt.Printf("%-20s %-50s %-15s\n", "ID", "NAME", "TYPE")
			fmt.Println(strings.Repeat("-", 90))
			for _, wf := range workflows {
				workflow := wf.(map[string]interface{})
				id := getString(workflow, "id")
				name := getString(workflow, "name")
				if len(name) > 47 {
					name = name[:47] + "..."
				}
				wfType := getString(workflow, "workflow_type")
				fmt.Printf("%-20s %-50s %-15s\n", id, name, wfType)
			}

			return nil
		},
	}

	return cmd
}

func newWorkflowShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <workflow-id>",
		Short: "Show workflow details",
		Long:  "Display detailed information about a specific workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/workflows/%s", args[0]), nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var workflow map[string]interface{}
			if err := json.Unmarshal(data, &workflow); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("ID:          %s\n", getString(workflow, "id"))
			fmt.Printf("Name:        %s\n", getString(workflow, "name"))
			fmt.Printf("Type:        %s\n", getString(workflow, "workflow_type"))
			fmt.Printf("Default:     %v\n", workflow["is_default"])
			fmt.Printf("Description: %s\n", getString(workflow, "description"))

			return nil
		},
	}

	return cmd
}

func newWorkflowStartCommand() *cobra.Command {
	var (
		workflowID string
		beadID     string
		projectID  string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a workflow",
		Long:  "Start a workflow execution for a bead",
		Example: `  loomctl workflow start --workflow=wf-ui-default --bead=loom-001 --project=loom-self`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workflowID == "" || beadID == "" || projectID == "" {
				return fmt.Errorf("--workflow, --bead, and --project are required")
			}

			client := newClient()
			data := map[string]interface{}{
				"workflow_id": workflowID,
				"bead_id":     beadID,
				"project_id":  projectID,
			}

			respData, err := client.post("/api/v1/workflows/start", data)
			if err != nil {
				return err
			}

			fmt.Printf("✅ Workflow started successfully!\n")

			if outputFormat == "json" {
				fmt.Println(string(respData))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&workflowID, "workflow", "w", "", "Workflow ID (required)")
	cmd.Flags().StringVarP(&beadID, "bead", "b", "", "Bead ID (required)")
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID (required)")

	cmd.MarkFlagRequired("workflow")
	cmd.MarkFlagRequired("bead")
	cmd.MarkFlagRequired("project")

	return cmd
}

// Agent commands
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents",
		Long:  "List and view agents in Loom",
	}

	cmd.AddCommand(newAgentListCommand())
	cmd.AddCommand(newAgentShowCommand())

	return cmd
}

func newAgentListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agents",
		Long:  "List all available agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/agents", nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var agents []map[string]interface{}
			if err := json.Unmarshal(data, &agents); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(agents) == 0 {
				fmt.Println("No agents found")
				return nil
			}

			// Table output
			fmt.Printf("%-50s %-30s %-10s\n", "ID", "NAME", "STATUS")
			fmt.Println(strings.Repeat("-", 95))
			for _, agent := range agents {
				id := getString(agent, "id")
				name := getString(agent, "name")
				status := getString(agent, "status")
				if status == "" {
					status = "unknown"
				}
				fmt.Printf("%-50s %-30s %-10s\n", id, name, status)
			}

			return nil
		},
	}

	return cmd
}

func newAgentShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <agent-id>",
		Short: "Show agent details",
		Long:  "Display detailed information about a specific agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/agents/%s", args[0]), nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var agent map[string]interface{}
			if err := json.Unmarshal(data, &agent); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("ID:       %s\n", getString(agent, "id"))
			fmt.Printf("Name:     %s\n", getString(agent, "name"))
			fmt.Printf("Role:     %s\n", getString(agent, "role"))
			fmt.Printf("Persona:  %s\n", getString(agent, "persona_name"))
			fmt.Printf("Status:   %s\n", getString(agent, "status"))

			return nil
		},
	}

	return cmd
}

// Project commands
func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
		Long:  "List and view projects in Loom",
	}

	cmd.AddCommand(newProjectListCommand())
	cmd.AddCommand(newProjectShowCommand())

	return cmd
}

func newProjectListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Long:  "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/projects", nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var projects []map[string]interface{}
			if err := json.Unmarshal(data, &projects); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(projects) == 0 {
				fmt.Println("No projects found")
				return nil
			}

			// Table output
			fmt.Printf("%-20s %-40s %-10s\n", "ID", "NAME", "STATUS")
			fmt.Println(strings.Repeat("-", 75))
			for _, project := range projects {
				id := getString(project, "id")
				name := getString(project, "name")
				if len(name) > 37 {
					name = name[:37] + "..."
				}
				status := getString(project, "status")
				fmt.Printf("%-20s %-40s %-10s\n", id, name, status)
			}

			return nil
		},
	}

	return cmd
}

func newProjectShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <project-id>",
		Short: "Show project details",
		Long:  "Display detailed information about a specific project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/projects/%s", args[0]), nil)
			if err != nil {
				return err
			}

			if outputFormat == "json" {
				fmt.Println(string(data))
				return nil
			}

			var project map[string]interface{}
			if err := json.Unmarshal(data, &project); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("ID:         %s\n", getString(project, "id"))
			fmt.Printf("Name:       %s\n", getString(project, "name"))
			fmt.Printf("Status:     %s\n", getString(project, "status"))
			fmt.Printf("Repository: %s\n", getString(project, "git_repo"))
			fmt.Printf("Branch:     %s\n", getString(project, "branch"))
			fmt.Printf("Work Dir:   %s\n", getString(project, "work_dir"))

			return nil
		},
	}

	return cmd
}

// Helper functions
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return 0
}
