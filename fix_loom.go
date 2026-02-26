package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	data, err := os.ReadFile("internal/loom/loom.go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
	content := string(data)

	// Change 1: Add ActionErrors field to ReplResult struct
	oldStruct := "type ReplResult struct {\n\tBeadID       string `json:\"bead_id\"`\n\tProviderID   string `json:\"provider_id\"`\n\tProviderName string `json:\"provider_name\"`\n\tModel        string `json:\"model\"`\n\tResponse     string `json:\"response\"`\n\tTokensUsed   int    `json:\"tokens_used\"`\n\tLatencyMs    int64  `json:\"latency_ms\"`\n}"
	newStruct := "type ReplResult struct {\n\tBeadID       string   `json:\"bead_id\"`\n\tProviderID   string   `json:\"provider_id\"`\n\tProviderName string   `json:\"provider_name\"`\n\tModel        string   `json:\"model\"`\n\tResponse     string   `json:\"response\"`\n\tTokensUsed   int      `json:\"tokens_used\"`\n\tLatencyMs    int64    `json:\"latency_ms\"`\n\tActionErrors []string `json:\"action_errors,omitempty\"`\n}"
	if strings.Contains(content, oldStruct) {
		content = strings.Replace(content, oldStruct, newStruct, 1)
		fmt.Println("Applied change 1: Added ActionErrors field to ReplResult")
	} else {
		fmt.Println("Warning: Could not find ReplResult struct to modify")
	}

	// Change 2: Fix ignored error from actionRouter.Execute
	oldExecute := "actionResults, _ = a.actionRouter.Execute(ctx, env, actx)"
	newExecute := `var execErr error
			actionResults, execErr = a.actionRouter.Execute(ctx, env, actx)
			if execErr != nil {
				log.Printf("Warning: Action execution failed: %v", execErr)
			}`
	if strings.Contains(content, oldExecute) {
		content = strings.Replace(content, oldExecute, newExecute, 1)
		fmt.Println("Applied change 2: Fixed ignored error from actionRouter.Execute")
	} else {
		fmt.Println("Warning: Could not find actionRouter.Execute call to modify")
	}

	err = os.WriteFile("internal/loom/loom.go", []byte(content), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("File updated successfully")
}
