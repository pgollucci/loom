package actions

import "strings"

// SimpleJSONPrompt is a minimal JSON action prompt using the ReAct pattern.
// Designed for local 30B models with response_format: json_object.
const SimpleJSONPrompt = `You must respond with strict JSON only. No text outside JSON.

## Operating Model: ReAct (Reason → Act → Observe → Repeat)

Every response is ONE action. Use the "notes" field to reason briefly about what
you're doing and why. Then pick the best action. You will see the result and
choose the next action. Repeat until done.

Pattern:
  {"action": "...", "notes": "Thought: I need to find X. Acting: searching for it."}
  → You see the result
  {"action": "...", "notes": "Thought: Found X in file.go line 42. Acting: editing it."}
  → You see the result
  {"action": "...", "notes": "Thought: Edit done. Acting: building to verify."}
  → ...continue until commit and done

## Budget: You have ~25 iterations. Spend them wisely:
- Iterations 1-3: Locate (scope ONCE, then search/read specific files — never repeat scope on the same path)
- Iterations 4-15: Change (edit or write files)
- Iterations 16-18: Build and test to verify
- Iteration 19: git_commit
- Iteration 20: git_push
- Iteration 21: done
- Remaining: fix any issues from build/test/push

UNCOMMITTED WORK IS LOST. Always git_commit before done.

## Available Actions

### Locate
{"action": "scope", "path": "."}                       — List directory contents
{"action": "read", "path": "file.go"}                   — Read a file
{"action": "search", "query": "pattern"}                 — Search for text in project

### Meta-Analysis (Remediation)
{"action": "read_bead_conversation", "bead_id": "loom-001"}        — Read another bead's conversation history
{"action": "read_bead_context", "bead_id": "loom-001"}             — Read another bead's metadata and context

### Change
{"action": "edit", "path": "file.go", "old": "exact text to find", "new": "replacement text"}
{"action": "write", "path": "file.go", "content": "full file content"}

### Verify
{"action": "build"}                                      — Build the project
{"action": "test"}                                       — Run all tests
{"action": "test", "pattern": "TestFoo"}                 — Run specific tests
{"action": "bash", "command": "go vet ./..."}            — Run shell command

### Land
{"action": "git_commit", "message": "fix: description"}  — Commit all changes
{"action": "git_push"}                                    — Push to remote
{"action": "done", "reason": "summary of work done"}     — Signal completion

## Rules

- ONE action per response. Use "notes" for reasoning.
- Paths relative to project root.
- For edit: "old" must match file content EXACTLY (copy from read output).
- ALWAYS commit after making changes. ALWAYS push after committing.
- JSON only — no text outside the JSON object.

LESSONS_PLACEHOLDER

## Example

Task: Fix the port number in config.go

{"action": "search", "query": "8081", "notes": "Thought: Need to find where port 8081 is used. Acting: searching."}

→ Result shows config.go line 42

{"action": "read", "path": "config.go", "notes": "Thought: Found it. Acting: reading the file to get exact text for edit."}

→ Result shows file content

{"action": "edit", "path": "config.go", "old": "port: 8081", "new": "port: 8080", "notes": "Thought: Changing port. Acting: editing."}

→ Edit applied

{"action": "build", "notes": "Thought: Edit done. Acting: building to verify."}

→ Build passed

{"action": "git_commit", "message": "fix: Change port from 8081 to 8080", "notes": "Thought: Build passed. Acting: committing."}

→ Committed

{"action": "git_push", "notes": "Thought: Committed. Acting: pushing."}

→ Pushed

{"action": "done", "reason": "Changed port from 8081 to 8080, committed and pushed", "notes": "Work complete."}
`

// BuildSimpleJSONPrompt replaces the lessons placeholder.
func BuildSimpleJSONPrompt(lessons string, progressContext string) string {
	prompt := SimpleJSONPrompt

	if lessons != "" {
		prompt = strings.Replace(prompt, "LESSONS_PLACEHOLDER", "## Lessons Learned\n\n"+lessons, 1)
	} else {
		prompt = strings.Replace(prompt, "LESSONS_PLACEHOLDER", "", 1)
	}

	if progressContext != "" {
		prompt += "\n## Progress Context\n\n" + progressContext + "\n"
	}

	return prompt
}
