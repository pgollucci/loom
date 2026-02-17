package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jordanhubbard/loom/pkg/models"
)

// UICaptureRequest represents a captured UI state from the bug reporter
type UICaptureRequest struct {
	URL         string                 `json:"url"`
	Timestamp   string                 `json:"timestamp"`
	UserAgent   string                 `json:"user_agent"`
	Viewport    map[string]interface{} `json:"viewport"`
	DOM         map[string]interface{} `json:"dom"`
	JavaScript  map[string]interface{} `json:"javascript"`
	Network     map[string]interface{} `json:"network"`
	State       map[string]interface{} `json:"state"`
	Description string                 `json:"description"`
}

// handleCaptureUI handles POST /api/v1/debug/capture-ui - capture UI state and auto-file bug
func (s *Server) handleCaptureUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var capture UICaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&capture); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if capture.Description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}

	// Build comprehensive bug report
	title := fmt.Sprintf("[UI Bug] %s", capture.Description)

	// Extract failed network requests
	failedRequests := ""
	if network, ok := capture.Network["failed"].([]interface{}); ok && len(network) > 0 {
		failedRequests = "\n### Failed Network Requests\n"
		for i, req := range network {
			if i >= 5 {
				failedRequests += fmt.Sprintf("...and %d more\n", len(network)-5)
				break
			}
			if reqMap, ok := req.(map[string]interface{}); ok {
				url := reqMap["url"]
				status := reqMap["status"]
				body := reqMap["body"]
				timestamp := reqMap["timestamp"]
				failedRequests += fmt.Sprintf("- **%v** → %v\n", url, status)
				if body != nil && body != "" {
					failedRequests += fmt.Sprintf("  Response: `%v`\n", body)
				}
				if timestamp != nil {
					failedRequests += fmt.Sprintf("  Time: %v\n", timestamp)
				}
			}
		}
	}

	// Extract JavaScript errors
	jsErrors := ""
	if js, ok := capture.JavaScript["errors"].([]interface{}); ok && len(js) > 0 {
		jsErrors = "\n### JavaScript Errors\n"
		for i, err := range js {
			if i >= 5 {
				jsErrors += fmt.Sprintf("...and %d more\n", len(js)-5)
				break
			}
			if errMap, ok := err.(map[string]interface{}); ok {
				message := errMap["message"]
				source := errMap["source"]
				line := errMap["line"]
				jsErrors += fmt.Sprintf("- **%v**\n", message)
				if source != nil && source != "" {
					jsErrors += fmt.Sprintf("  at %v:%v\n", source, line)
				}
			}
		}
	}

	// Extract loading/error elements from DOM
	domIssues := ""
	if dom, ok := capture.DOM["loadingElements"].([]interface{}); ok && len(dom) > 0 {
		domIssues += "\n### Stuck Loading State Detected\n"
		for i, el := range dom {
			if i >= 3 {
				break
			}
			if elMap, ok := el.(map[string]interface{}); ok {
				domIssues += fmt.Sprintf("- %v#%v: %v\n", elMap["tag"], elMap["id"], elMap["text"])
			}
		}
	}

	if dom, ok := capture.DOM["errorElements"].([]interface{}); ok && len(dom) > 0 {
		domIssues += "\n### Error Elements in DOM\n"
		for i, el := range dom {
			if i >= 3 {
				break
			}
			if elMap, ok := el.(map[string]interface{}); ok {
				if visible, ok := elMap["visible"].(bool); ok && visible {
					domIssues += fmt.Sprintf("- %v.%v: %v\n", elMap["tag"], elMap["class"], elMap["text"])
				}
			}
		}
	}

	description := fmt.Sprintf(`User-reported UI bug via headless bug reporter.

## User Description
%s

## Page URL
%s

## Timestamp
%s

## Browser
%s

## Viewport
%dx%d
%s%s%s

## Root Cause Analysis Needed
- [ ] Check API endpoint availability
- [ ] Verify database migrations
- [ ] Check JavaScript console for errors
- [ ] Inspect network requests
- [ ] Verify data is being loaded

## Full DOM Snapshot
Available in bead context (key: ui_capture)

---
*This bug was automatically filed by the headless UI debugger. No screenshots required - full state captured.*
`,
		capture.Description,
		capture.URL,
		capture.Timestamp,
		capture.UserAgent,
		getIntFromMap(capture.Viewport, "width"),
		getIntFromMap(capture.Viewport, "height"),
		failedRequests,
		jsErrors,
		domIssues,
	)

	// Create bead with P0 priority
	bead, err := s.app.CreateBead(
		title,
		description,
		models.BeadPriorityP0,
		"bug",
		"loom-self", // TODO: Get from capture.URL or config
	)
	if err != nil {
		http.Error(w, "Failed to create bead: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store full capture in bead context for detailed analysis
	captureJSON, _ := json.Marshal(capture)
	context := map[string]string{"ui_capture": string(captureJSON)}
	if _, err := s.app.UpdateBead(bead.ID, map[string]interface{}{"context": context}); err != nil {
		// Log but don't fail - bead was created successfully
		fmt.Printf("Warning: Failed to store UI capture in bead context: %v\n", err)
	}

	// Add tags for better filtering
	tags := []string{"ui-bug", "auto-filed", "headless-debugger"}
	if failedRequests != "" {
		tags = append(tags, "api-error")
	}
	if jsErrors != "" {
		tags = append(tags, "javascript-error")
	}
	if domIssues != "" {
		tags = append(tags, "loading-stuck")
	}

	if _, err := s.app.UpdateBead(bead.ID, map[string]interface{}{"tags": tags}); err != nil {
		fmt.Printf("Warning: Failed to add tags to bead: %v\n", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"bead_id":   bead.ID,
		"title":     title,
		"priority":  "P0",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// Helper to safely extract int from map
func getIntFromMap(m map[string]interface{}, key string) int {
	if val, ok := m[key]; ok {
		if intVal, ok := val.(float64); ok {
			return int(intVal)
		}
	}
	return 0
}
