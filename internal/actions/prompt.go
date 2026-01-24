package actions

const ActionPrompt = `
You must respond with strict JSON only. Do not include any surrounding text.

The response must be a single JSON object with this shape:
{
  "actions": [
    {
      "type": "ask_followup|read_code|edit_code|read_file|read_tree|search_text|apply_patch|git_status|git_diff|run_command|create_bead|escalate_ceo",
      "question": "string",
      "path": "string",
      "patch": "string",
      "query": "string",
      "max_depth": 2,
      "limit": 100,
      "command": "string",
      "working_dir": "string",
      "bead": {
        "title": "string",
        "description": "string",
        "priority": 0,
        "type": "task",
        "project_id": "string",
        "tags": ["string"]
      },
      "bead_id": "string",
      "reason": "string",
      "returned_to": "string"
    }
  ],
  "notes": "string"
}

Only include fields required for the selected action type.
Paths are always relative to the project root.
`
