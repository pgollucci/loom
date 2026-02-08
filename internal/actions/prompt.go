package actions

import "strings"

const ActionPrompt = `
You must respond with strict JSON only. Do not include any surrounding text or model reasoning markers (e.g. <think>).

The response must be a single JSON object with this shape:
{
  "actions": [
    {
      "type": "<action_type>",
      ...fields for the selected action type...
    }
  ],
  "notes": "string"
}

## Action Types

### File Operations
- read_file / read_code: Read file contents. Required: path
- write_file: Write entire file contents. Required: path, content (PREFERRED for code changes)
- edit_code / apply_patch: Apply unified diff patch. Required: path, patch (unified diff format)
- read_tree: List directory structure. Required: path. Optional: max_depth, limit
- search_text: Search for text/regex in files. Required: query. Optional: path, limit
- move_file: Move/rename file. Required: source_path, target_path
- delete_file: Delete a file. Required: path
- rename_file: Rename a file. Required: source_path, new_name

### Build & Test
- build_project: Build the project. Optional: build_target, build_command, framework, timeout_seconds
- run_tests: Run test suite. Optional: test_pattern, framework, timeout_seconds
- run_linter: Run linter. Optional: files, framework, timeout_seconds
- run_command: Execute shell command. Required: command. Optional: working_dir

### Git Operations
- git_status: Show working tree status
- git_diff: Show unstaged changes
- git_commit: Create a commit. Optional: commit_message, files
- git_push: Push to remote. Optional: branch, set_upstream
- git_log: View commit history. Optional: branch, max_count
- git_fetch: Fetch from remote
- git_checkout: Switch branches. Required: branch
- git_merge: Merge a branch. Required: source_branch. Optional: commit_message, no_ff
- git_revert: Revert commits. Required: commit_sha or commit_shas. Optional: reason
- git_list_branches: List all branches
- git_diff_branches: Diff two branches. Required: source_branch, target_branch
- git_bead_commits: Get commits for the current bead

### Bead Management
- create_bead: Create a work item. Required: bead object with title, project_id
- close_bead: Close/complete a bead. Required: bead_id. Optional: reason
- escalate_ceo: Escalate to CEO for decision. Required: bead_id, reason
- done: Signal that work is complete — no more actions needed. Optional: reason

### Code Navigation (when LSP is available)
- find_references: Find all references. Required: path + (symbol or line+column)
- go_to_definition: Go to symbol definition. Required: path + (symbol or line+column)
- find_implementations: Find implementations. Required: path + (symbol or line+column)

### Agent Communication
- send_agent_message: Send message to another agent. Required: to_agent_id or to_agent_role, message_type
- delegate_task: Delegate work to another agent. Required: delegate_to_role, task_title

## Code Change Workflow

When making code changes, follow this sequence:
1. **Read** — Read relevant files to understand the codebase
2. **Plan** — Decide what changes are needed (explain in notes)
3. **Edit** — Make changes using write_file (preferred) or apply_patch
4. **Build** — Run build_project to verify compilation
5. **Fix** — If build fails, read error output, fix the issues, rebuild
6. **Test** — Run run_tests to verify behavior
7. **Commit** — Use git_commit with a clear message
8. **Close** — Use close_bead or done when finished

## Important Rules

- Always verify changes compile before committing
- If a build or test fails, fix the errors before proceeding
- Use search_text to find code before making blind edits
- Include clear, descriptive commit messages
- Use read_tree to understand project structure before making changes
- For code changes, PREFER write_file over edit_code/apply_patch
- Paths are always relative to the project root
- Only include fields required for the selected action type

LESSONS_PLACEHOLDER

## Writing Code Changes

Example write_file:
{
  "actions": [{"type": "write_file", "path": "src/config.go", "content": "package src\n\nconst Version = \"1.0.0\"\n"}],
  "notes": "Updated version"
}

Example build + fix cycle:
{
  "actions": [{"type": "build_project"}],
  "notes": "Verifying the changes compile"
}

Example signaling completion:
{
  "actions": [{"type": "close_bead", "bead_id": "BEAD_ID", "reason": "Changes implemented and verified"}],
  "notes": "All changes compiled, tests pass, committed"
}
`

// BuildEnhancedPrompt replaces the lessons placeholder with actual lessons
// and appends any progress context from prior dispatches.
func BuildEnhancedPrompt(lessons string, progressContext string) string {
	prompt := ActionPrompt

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
