package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const version = "2.0.0"

var (
	serverURL    string
	outputFormat string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "loomctl",
		Short: "Loom CLI - interact with your Loom server",
		Long: `loomctl is a command-line interface for interacting with Loom servers.
All output is structured JSON by default (pipe through jq for human-readable formatting).`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", getDefaultServer(), "Loom server URL")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", "Output format: json, table")

	// Add subcommands
	rootCmd.AddCommand(newBeadCommand())
	rootCmd.AddCommand(newWorkflowCommand())
	rootCmd.AddCommand(newAgentCommand())
	rootCmd.AddCommand(newProjectCommand())
	rootCmd.AddCommand(newProviderCommand())
	rootCmd.AddCommand(newLogCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newMetricsCommand())
	rootCmd.AddCommand(newConversationCommand())
	rootCmd.AddCommand(newAnalyticsCommand())
	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newEventCommand())

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

// --- HTTP client ---

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func newClient() *Client {
	return &Client{
		BaseURL: serverURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, params url.Values, data interface{}) ([]byte, error) {
	u := fmt.Sprintf("%s%s", c.BaseURL, path)
	if params != nil {
		u += "?" + params.Encode()
	}

	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data: %w", err)
		}
		body = strings.NewReader(string(jsonData))
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

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

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	return c.do("GET", path, params, nil)
}

func (c *Client) post(path string, data interface{}) ([]byte, error) {
	return c.do("POST", path, nil, data)
}

func (c *Client) put(path string, data interface{}) ([]byte, error) {
	return c.do("PUT", path, nil, data)
}

func (c *Client) delete(path string) ([]byte, error) {
	return c.do("DELETE", path, nil, nil)
}

// streamSSE reads an SSE stream and prints each event's data field as JSON.
func (c *Client) streamSSE(path string) error {
	u := fmt.Sprintf("%s%s", c.BaseURL, path)
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			fmt.Println(line[6:])
		}
	}
	return scanner.Err()
}

// outputJSON prints raw JSON data. All commands use this as the primary output path.
func outputJSON(data []byte) {
	// Pretty-print the JSON
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		// Not valid JSON, print raw
		fmt.Println(string(data))
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// --- Bead commands ---

func newBeadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bead",
		Short: "Manage beads (work items)",
	}
	cmd.AddCommand(newBeadListCommand())
	cmd.AddCommand(newBeadCreateCommand())
	cmd.AddCommand(newBeadShowCommand())
	cmd.AddCommand(newBeadClaimCommand())
	cmd.AddCommand(newBeadPokeCommand())
	cmd.AddCommand(newBeadUpdateCommand())
	cmd.AddCommand(newBeadDeleteCommand())
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
		Example: `  loomctl bead list
  loomctl bead list --status=open --project=loom-self`,
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
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Filter by project ID")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status")
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
		Use:     "create",
		Short:   "Create a new bead",
		Example: `  loomctl bead create --title="Fix bug" --project=loom-self`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			body := map[string]interface{}{
				"title":       title,
				"description": description,
				"priority":    priority,
				"project_id":  projectID,
			}
			if beadType != "" {
				body["type"] = beadType
			}
			data, err := client.post("/api/v1/beads", body)
			if err != nil {
				return err
			}
			outputJSON(data)
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
	return &cobra.Command{
		Use:     "show <bead-id>",
		Short:   "Show bead details",
		Args:    cobra.ExactArgs(1),
		Example: `  loomctl bead show loom-001`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/beads/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newBeadClaimCommand() *cobra.Command {
	var agentID string
	cmd := &cobra.Command{
		Use:   "claim <bead-id>",
		Short: "Claim a bead for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.post(fmt.Sprintf("/api/v1/beads/%s/claim", args[0]), map[string]interface{}{"agent_id": agentID})
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVarP(&agentID, "agent", "a", "", "Agent ID (required)")
	cmd.MarkFlagRequired("agent")
	return cmd
}

func newBeadPokeCommand() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:     "poke <bead-id>",
		Short:   "Redispatch a stuck bead",
		Args:    cobra.ExactArgs(1),
		Example: `  loomctl bead poke loom-001 --reason="fixes deployed"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			body := map[string]interface{}{}
			if reason != "" {
				body["reason"] = reason
			}
			data, err := client.post(fmt.Sprintf("/api/v1/beads/%s/redispatch", args[0]), body)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVarP(&reason, "reason", "r", "", "Reason for redispatch")
	return cmd
}

func newBeadUpdateCommand() *cobra.Command {
	var (
		status   string
		priority int
		title    string
	)
	cmd := &cobra.Command{
		Use:   "update <bead-id>",
		Short: "Update bead fields",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			body := map[string]interface{}{}
			if cmd.Flags().Changed("status") {
				body["status"] = status
			}
			if cmd.Flags().Changed("priority") {
				body["priority"] = priority
			}
			if cmd.Flags().Changed("title") {
				body["title"] = title
			}
			data, err := client.put(fmt.Sprintf("/api/v1/beads/%s", args[0]), body)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "New status")
	cmd.Flags().IntVar(&priority, "priority", 0, "New priority")
	cmd.Flags().StringVar(&title, "title", "", "New title")
	return cmd
}

func newBeadDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bead-id>",
		Short: "Delete a bead",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.delete(fmt.Sprintf("/api/v1/beads/%s", args[0]))
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Provider commands ---

func newProviderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage LLM providers",
	}
	cmd.AddCommand(newProviderListCommand())
	cmd.AddCommand(newProviderShowCommand())
	cmd.AddCommand(newProviderRegisterCommand())
	cmd.AddCommand(newProviderDeleteCommand())
	return cmd
}

func newProviderListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/providers", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newProviderShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <provider-id>",
		Short: "Show provider details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/providers/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newProviderRegisterCommand() *cobra.Command {
	var (
		name     string
		pType    string
		endpoint string
		model    string
		apiKey   string
	)
	cmd := &cobra.Command{
		Use:   "register <provider-id>",
		Short: "Register or update a provider",
		Args:  cobra.ExactArgs(1),
		Example: `  loomctl provider register sparky-local \
    --name="Sparky Local" --type=openai \
    --endpoint="http://sparky.local:8000/v1" \
    --model="Qwen/Qwen2.5-Coder-32B-Instruct"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			body := map[string]interface{}{
				"name":     name,
				"type":     pType,
				"endpoint": endpoint,
				"model":    model,
			}
			if apiKey != "" {
				body["api_key"] = apiKey
			}
			data, err := client.put(fmt.Sprintf("/api/v1/providers/%s", args[0]), body)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Provider display name (required)")
	cmd.Flags().StringVar(&pType, "type", "openai", "Provider type (openai, anthropic, local)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "API endpoint URL (required)")
	cmd.Flags().StringVar(&model, "model", "", "Model ID (required)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key (optional)")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("endpoint")
	cmd.MarkFlagRequired("model")
	return cmd
}

func newProviderDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <provider-id>",
		Short: "Delete a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.delete(fmt.Sprintf("/api/v1/providers/%s", args[0]))
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Log commands ---

func newLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "View and stream logs",
	}
	cmd.AddCommand(newLogRecentCommand())
	cmd.AddCommand(newLogStreamCommand())
	cmd.AddCommand(newLogExportCommand())
	return cmd
}

func newLogRecentCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "recent",
		Short:   "Show recent log entries",
		Example: `  loomctl log recent --limit=50`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			params := url.Values{}
			if limit > 0 {
				params.Set("limit", fmt.Sprintf("%d", limit))
			}
			data, err := client.get("/api/v1/logs/recent", params)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "Number of log entries")
	return cmd
}

func newLogStreamCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stream",
		Short: "Stream live logs (SSE)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			return client.streamSSE("/api/v1/logs/stream")
		},
	}
}

func newLogExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export all logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/logs/export", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Status command ---

func newStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show system status overview",
		Long:  "Aggregates health, providers, agents, and bead counts into one JSON object",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			result := map[string]interface{}{}

			// Health
			if data, err := client.get("/api/v1/health", nil); err == nil {
				var v interface{}
				if json.Unmarshal(data, &v) == nil {
					result["health"] = v
				}
			}

			// Providers
			if data, err := client.get("/api/v1/providers", nil); err == nil {
				var providers []interface{}
				if json.Unmarshal(data, &providers) == nil {
					healthy, failed, total := 0, 0, len(providers)
					for _, p := range providers {
						pm, _ := p.(map[string]interface{})
						if pm["status"] == "healthy" {
							healthy++
						} else if pm["status"] == "failed" {
							failed++
						}
					}
					result["providers"] = map[string]interface{}{
						"total":   total,
						"healthy": healthy,
						"failed":  failed,
						"list":    providers,
					}
				}
			}

			// Agents
			if data, err := client.get("/api/v1/agents", nil); err == nil {
				var agents []interface{}
				if json.Unmarshal(data, &agents) == nil {
					working, idle := 0, 0
					for _, a := range agents {
						am, _ := a.(map[string]interface{})
						if am["status"] == "working" {
							working++
						} else if am["status"] == "idle" {
							idle++
						}
					}
					result["agents"] = map[string]interface{}{
						"total":   len(agents),
						"working": working,
						"idle":    idle,
					}
				}
			}

			// Beads
			if data, err := client.get("/api/v1/beads", nil); err == nil {
				var beads []interface{}
				if json.Unmarshal(data, &beads) == nil {
					counts := map[string]int{}
					for _, b := range beads {
						bm, _ := b.(map[string]interface{})
						s, _ := bm["status"].(string)
						counts[s]++
					}
					result["beads"] = map[string]interface{}{
						"total":    len(beads),
						"by_status": counts,
					}
				}
			}

			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
	return cmd
}

// --- Metrics / Observability commands ---

func newMetricsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Retrieve observability metrics",
	}
	cmd.AddCommand(newMetricsPrometheusCommand())
	cmd.AddCommand(newMetricsCacheCommand())
	cmd.AddCommand(newMetricsPatternsCommand())
	cmd.AddCommand(newMetricsEventsStatsCommand())
	return cmd
}

func newMetricsPrometheusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prometheus",
		Short: "Fetch raw Prometheus metrics from the /metrics endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/metrics", nil)
			if err != nil {
				return err
			}
			// Prometheus metrics are text, wrap in JSON
			out, _ := json.MarshalIndent(map[string]interface{}{
				"format": "prometheus",
				"raw":    string(data),
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func newMetricsCacheCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cache",
		Short: "Show cache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/cache/stats", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newMetricsPatternsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "patterns",
		Short: "Show pattern analysis results",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/patterns/analysis", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newMetricsEventsStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Show event statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/events/stats", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Conversation commands ---

func newConversationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "conversation",
		Aliases: []string{"conv"},
		Short:   "View agent conversations",
	}
	cmd.AddCommand(newConversationListCommand())
	cmd.AddCommand(newConversationShowCommand())
	return cmd
}

func newConversationListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List conversation sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/conversations", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newConversationShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show conversation messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/conversations/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Analytics commands ---

func newAnalyticsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "View analytics and cost data",
	}
	cmd.AddCommand(newAnalyticsStatsCommand())
	cmd.AddCommand(newAnalyticsCostsCommand())
	cmd.AddCommand(newAnalyticsLogsCommand())
	cmd.AddCommand(newAnalyticsExportCommand())
	return cmd
}

func newAnalyticsStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show analytics statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/analytics/stats", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newAnalyticsCostsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "costs",
		Short: "Show cost breakdown by provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/analytics/costs", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newAnalyticsLogsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Show analytics log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/analytics/logs", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newAnalyticsExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export analytics data",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/analytics/export", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Config commands ---

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage server configuration",
	}
	cmd.AddCommand(newConfigShowCommand())
	cmd.AddCommand(newConfigExportCommand())
	return cmd
}

func newConfigShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/config", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newConfigExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export configuration as YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/config/export.yaml", nil)
			if err != nil {
				return err
			}
			// YAML content, wrap in JSON
			out, _ := json.MarshalIndent(map[string]interface{}{
				"format":  "yaml",
				"content": string(data),
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

// --- Event commands ---

func newEventCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "View events and activity feed",
	}
	cmd.AddCommand(newEventListCommand())
	cmd.AddCommand(newEventStreamCommand())
	cmd.AddCommand(newEventActivityCommand())
	return cmd
}

func newEventListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recent events",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/events", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newEventStreamCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stream",
		Short: "Stream events in real-time (SSE)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			return client.streamSSE("/api/v1/events/stream")
		},
	}
}

func newEventActivityCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "activity",
		Short: "Show activity feed",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/activity-feed", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Workflow commands ---

func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
	}
	cmd.AddCommand(newWorkflowListCommand())
	cmd.AddCommand(newWorkflowShowCommand())
	cmd.AddCommand(newWorkflowStartCommand())
	cmd.AddCommand(newWorkflowExecutionsCommand())
	cmd.AddCommand(newWorkflowAnalyticsCommand())
	return cmd
}

func newWorkflowListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/workflows", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newWorkflowShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "show <workflow-id>",
		Short: "Show workflow details",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/workflows/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newWorkflowStartCommand() *cobra.Command {
	var (
		workflowID string
		beadID     string
		projectID  string
	)
	cmd := &cobra.Command{
		Use:     "start",
		Short:   "Start a workflow execution",
		Example: `  loomctl workflow start --workflow=wf-ui-default --bead=loom-001 --project=loom-self`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.post("/api/v1/workflows/start", map[string]interface{}{
				"workflow_id": workflowID,
				"bead_id":     beadID,
				"project_id":  projectID,
			})
			if err != nil {
				return err
			}
			outputJSON(data)
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

func newWorkflowExecutionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "executions",
		Short: "List workflow executions",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/workflows/executions", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newWorkflowAnalyticsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "analytics",
		Short: "Show workflow analytics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/workflows/analytics", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Agent commands ---

func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents",
	}
	cmd.AddCommand(newAgentListCommand())
	cmd.AddCommand(newAgentShowCommand())
	return cmd
}

func newAgentListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/agents", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newAgentShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <agent-id>",
		Short: "Show agent details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/agents/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

// --- Project commands ---

func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	cmd.AddCommand(newProjectListCommand())
	cmd.AddCommand(newProjectShowCommand())
	return cmd
}

func newProjectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get("/api/v1/projects", nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}

func newProjectShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <project-id>",
		Short: "Show project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := newClient()
			data, err := client.get(fmt.Sprintf("/api/v1/projects/%s", args[0]), nil)
			if err != nil {
				return err
			}
			outputJSON(data)
			return nil
		},
	}
}
