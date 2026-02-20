package projectagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/internal/provider"
)

// ActionLoopConfig configures the multi-turn action loop
type ActionLoopConfig struct {
	MaxIterations     int
	ProviderEndpoint  string
	ProviderModel     string
	ProviderAPIKey    string
	PersonaInstructions string
}

// ActionResult captures the outcome of a single action
type ActionResult struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// LLMAction is a structured action returned by the LLM
type LLMAction struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params,omitempty"`
}

// LLMResponse is the parsed JSON response from the LLM
type LLMResponse struct {
	Thinking string      `json:"thinking,omitempty"`
	Actions  []LLMAction `json:"actions"`
}

// RunActionLoop executes the multi-turn action loop:
// prompt LLM -> parse actions -> execute -> format feedback -> repeat until done.
func (a *Agent) RunActionLoop(ctx context.Context, taskTitle, taskDescription string, loopCfg ActionLoopConfig) (string, error) {
	if loopCfg.MaxIterations <= 0 {
		loopCfg.MaxIterations = 20
	}

	client := provider.NewOpenAIProvider(loopCfg.ProviderEndpoint, loopCfg.ProviderAPIKey)

	messages := []provider.ChatMessage{
		{Role: "system", Content: a.buildSystemPrompt(loopCfg)},
		{Role: "user", Content: fmt.Sprintf("Task: %s\n\nDescription: %s\n\nBegin working. Respond with JSON containing an \"actions\" array.", taskTitle, taskDescription)},
	}

	var lastOutput string
	consecutiveParseFailures := 0

	for iteration := 0; iteration < loopCfg.MaxIterations; iteration++ {
		log.Printf("[ActionLoop] Iteration %d/%d for task: %s", iteration+1, loopCfg.MaxIterations, taskTitle)

		resp, err := client.CreateChatCompletion(ctx, &provider.ChatCompletionRequest{
			Model:    loopCfg.ProviderModel,
			Messages: messages,
		})
		if err != nil {
			return lastOutput, fmt.Errorf("LLM request failed on iteration %d: %w", iteration+1, err)
		}

		if len(resp.Choices) == 0 {
			return lastOutput, fmt.Errorf("empty response from LLM on iteration %d", iteration+1)
		}

		responseText := resp.Choices[0].Message.Content
		messages = append(messages, provider.ChatMessage{Role: "assistant", Content: responseText})

		llmResp, err := parseActionsFromResponse(responseText)
		if err != nil {
			consecutiveParseFailures++
			if consecutiveParseFailures >= 2 {
				return lastOutput, fmt.Errorf("2 consecutive parse failures, stopping loop")
			}
			messages = append(messages, provider.ChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("Failed to parse your response as JSON: %v. Please respond with valid JSON containing an \"actions\" array.", err),
			})
			continue
		}
		consecutiveParseFailures = 0

		if len(llmResp.Actions) == 0 {
			return lastOutput, nil
		}

		var feedback strings.Builder
		done := false

		for _, action := range llmResp.Actions {
			if action.Type == "done" || action.Type == "close_bead" {
				done = true
				if msg, ok := action.Params["message"].(string); ok {
					lastOutput = msg
				}
				break
			}

			result := a.executeAction(ctx, action)
			feedback.WriteString(formatActionFeedback(action, result))
			feedback.WriteString("\n\n")

			if result.Output != "" {
				lastOutput = result.Output
			}
		}

		if done {
			log.Printf("[ActionLoop] Task completed after %d iterations", iteration+1)
			return lastOutput, nil
		}

		messages = append(messages, provider.ChatMessage{
			Role:    "user",
			Content: feedback.String(),
		})
	}

	return lastOutput, fmt.Errorf("max iterations (%d) reached", loopCfg.MaxIterations)
}

func (a *Agent) buildSystemPrompt(cfg ActionLoopConfig) string {
	var sb strings.Builder
	sb.WriteString("You are an autonomous agent working in a software project. ")

	if a.role != "" {
		sb.WriteString(fmt.Sprintf("Your role is: %s. ", a.role))
	}

	if cfg.PersonaInstructions != "" {
		sb.WriteString("\n\n")
		sb.WriteString(cfg.PersonaInstructions)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`Respond with JSON containing an "actions" array. Available action types:
- {"type": "bash", "params": {"command": "..."}} - Execute a shell command
- {"type": "read", "params": {"path": "..."}} - Read a file
- {"type": "write", "params": {"path": "...", "content": "..."}} - Write a file
- {"type": "install", "params": {"packages": ["pkg1", "pkg2"]}} - Install OS packages (auto-detects apt/apk)
- {"type": "git_commit", "params": {"message": "..."}} - Commit changes
- {"type": "git_push", "params": {}} - Push to remote
- {"type": "search_text", "params": {"pattern": "...", "path": "..."}} - Search files
- {"type": "done", "params": {"message": "..."}} - Signal completion

You are running inside an isolated container as root. If a command fails with "command not found"
or a missing tool error, use the "install" action to install the required package, then retry.
Always use the "done" action when finished.`)

	return sb.String()
}

// isCommandNotFound checks if an error output indicates a missing tool/command.
func isCommandNotFound(output string) bool {
	lower := strings.ToLower(output)
	patterns := []string{
		"command not found",
		"not found",
		"no such file or directory",
		"unable to locate package",
		"exec format error",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (a *Agent) executeAction(ctx context.Context, action LLMAction) ActionResult {
	switch action.Type {
	case "bash":
		output, err := a.executeBash(ctx, action.Params)
		if err != nil {
			errStr := err.Error()
			combined := errStr + " " + output
			if isCommandNotFound(combined) {
				return ActionResult{
					Type: "bash", Success: false, Output: output,
					Error: errStr + "\n\nHINT: The required tool is not installed. Use {\"type\": \"install\", \"params\": {\"packages\": [\"<package-name>\"]}} to install it, then retry this command.",
				}
			}
			return ActionResult{Type: "bash", Success: false, Error: errStr, Output: output}
		}
		return ActionResult{Type: "bash", Success: true, Output: output}

	case "read":
		output, err := a.executeRead(ctx, action.Params)
		if err != nil {
			return ActionResult{Type: "read", Success: false, Error: err.Error()}
		}
		return ActionResult{Type: "read", Success: true, Output: output}

	case "write":
		output, err := a.executeWrite(ctx, action.Params)
		if err != nil {
			return ActionResult{Type: "write", Success: false, Error: err.Error()}
		}
		return ActionResult{Type: "write", Success: true, Output: output}

	case "install":
		output, err := a.executeInstall(ctx, action.Params)
		if err != nil {
			return ActionResult{Type: "install", Success: false, Error: err.Error(), Output: output}
		}
		return ActionResult{Type: "install", Success: true, Output: output}

	case "git_commit":
		output, err := a.executeGitCommit(ctx, action.Params)
		if err != nil {
			return ActionResult{Type: "git_commit", Success: false, Error: err.Error(), Output: output}
		}
		return ActionResult{Type: "git_commit", Success: true, Output: output}

	case "git_push":
		output, err := a.executeGitPush(ctx, action.Params)
		if err != nil {
			return ActionResult{Type: "git_push", Success: false, Error: err.Error(), Output: output}
		}
		return ActionResult{Type: "git_push", Success: true, Output: output}

	case "search_text":
		pattern, _ := action.Params["pattern"].(string)
		path, _ := action.Params["path"].(string)
		if path == "" {
			path = "."
		}
		output, err := a.executeBash(ctx, map[string]interface{}{
			"command": fmt.Sprintf("grep -rn %q %s 2>/dev/null | head -50", pattern, path),
		})
		if err != nil {
			return ActionResult{Type: "search_text", Success: false, Error: err.Error(), Output: output}
		}
		return ActionResult{Type: "search_text", Success: true, Output: output}

	default:
		return ActionResult{Type: action.Type, Success: false, Error: fmt.Sprintf("unsupported action type: %s", action.Type)}
	}
}

// executeInstall installs OS packages, auto-detecting apt vs apk.
func (a *Agent) executeInstall(ctx context.Context, params map[string]interface{}) (string, error) {
	var packages []string
	if pkgs, ok := params["packages"].([]interface{}); ok {
		for _, p := range pkgs {
			if s, ok := p.(string); ok && s != "" {
				packages = append(packages, s)
			}
		}
	}
	if cmd, ok := params["command"].(string); ok && cmd != "" {
		return a.executeBash(ctx, map[string]interface{}{"command": cmd})
	}
	if len(packages) == 0 {
		return "", fmt.Errorf("install requires packages list or command")
	}

	// Detect package manager
	detectCmd := "test -f /etc/alpine-release && echo alpine || echo debian"
	osOutput, err := a.executeBash(ctx, map[string]interface{}{"command": detectCmd})
	if err != nil {
		osOutput = "debian"
	}
	osOutput = strings.TrimSpace(osOutput)

	var installCmd string
	pkgList := strings.Join(packages, " ")
	if strings.Contains(osOutput, "alpine") {
		installCmd = "apk add --no-cache " + pkgList
	} else {
		installCmd = "apt-get update -qq && apt-get install -y --no-install-recommends " + pkgList
	}

	return a.executeBash(ctx, map[string]interface{}{"command": installCmd})
}

func parseActionsFromResponse(text string) (*LLMResponse, error) {
	text = strings.TrimSpace(text)

	// Try direct parse
	var resp LLMResponse
	if err := json.Unmarshal([]byte(text), &resp); err == nil {
		return &resp, nil
	}

	// Try extracting JSON from markdown code block
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		end := strings.Index(text[start:], "```")
		if end > 0 {
			jsonStr := strings.TrimSpace(text[start : start+end])
			if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	// Try extracting any JSON object
	if idx := strings.Index(text, "{"); idx >= 0 {
		jsonStr := text[idx:]
		depth := 0
		for i, ch := range jsonStr {
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := jsonStr[:i+1]
					if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
						return &resp, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("no valid JSON found in response")
}

func formatActionFeedback(action LLMAction, result ActionResult) string {
	status := "SUCCESS"
	if !result.Success {
		status = "FAILED"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Action: %s [%s]\n", action.Type, status))

	if result.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", result.Error))
	}

	if result.Output != "" {
		output := result.Output
		if len(output) > 4000 {
			output = output[:2000] + "\n... (truncated) ...\n" + output[len(output)-2000:]
		}
		sb.WriteString(fmt.Sprintf("Output:\n%s", output))
	}

	return sb.String()
}

// ensure time is imported
var _ = time.Now
