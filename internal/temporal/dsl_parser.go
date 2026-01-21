package temporal

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// ParseTemporalDSL extracts and parses Temporal DSL blocks from text
// Returns: parsed instructions, cleaned text with DSL removed, error
func ParseTemporalDSL(text string) ([]TemporalInstruction, string, error) {
	if text == "" {
		return nil, text, nil
	}

	// Extract all <temporal>...</temporal> blocks
	re := regexp.MustCompile(`(?s)<temporal>\s*(.*?)\s*</temporal>`)
	matches := re.FindAllStringSubmatch(text, -1)

	if len(matches) == 0 {
		// No temporal blocks found
		return nil, text, nil
	}

	var instructions []TemporalInstruction
	cleanedText := text

	// Parse each block
	for _, match := range matches {
		block := match[1]
		parsed, err := parseTemporalBlock(block)
		if err != nil {
			log.Printf("Error parsing temporal block: %v", err)
			continue
		}
		instructions = append(instructions, parsed...)

		// Remove this block from the text
		cleanedText = strings.Replace(cleanedText, match[0], "", 1)
	}

	// Clean up extra whitespace
	cleanedText = strings.TrimSpace(cleanedText)
	cleanedText = regexp.MustCompile(`\n\n+`).ReplaceAllString(cleanedText, "\n\n")

	return instructions, cleanedText, nil
}

// parseTemporalBlock parses a single temporal block containing multiple instructions
func parseTemporalBlock(block string) ([]TemporalInstruction, error) {
	var instructions []TemporalInstruction

	// Split by "END" keyword to get individual instructions
	entries := strings.Split(block, "END")

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		instr, err := parseTemporalInstruction(entry)
		if err != nil {
			log.Printf("Error parsing instruction: %v", err)
			continue
		}

		if instr != nil {
			instructions = append(instructions, *instr)
		}
	}

	return instructions, nil
}

// parseTemporalInstruction parses a single temporal instruction
func parseTemporalInstruction(text string) (*TemporalInstruction, error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty instruction")
	}

	firstLine := strings.TrimSpace(lines[0])

	// Split on colon to handle "WORKFLOW: name" format
	headerParts := strings.SplitN(firstLine, ":", 2)
	if len(headerParts) < 2 {
		return nil, fmt.Errorf("invalid instruction header: %s", firstLine)
	}

	instrType := TemporalInstructionType(strings.TrimSpace(strings.ToUpper(headerParts[0])))
	name := strings.TrimSpace(headerParts[1])

	instr := &TemporalInstruction{
		Type:       instrType,
		Name:       name,
		Input:      make(map[string]interface{}),
		SignalData: make(map[string]interface{}),
	}

	// Parse instruction parameters
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "END" {
			continue
		}

		if !strings.Contains(line, ":") {
			continue
		}

		keyValue := strings.SplitN(line, ":", 2)
		if len(keyValue) != 2 {
			continue
		}

		key := strings.TrimSpace(keyValue[0])
		value := strings.TrimSpace(keyValue[1])

		switch strings.ToUpper(key) {
		case "INPUT":
			if err := parseJSONInput(value, instr.Input); err != nil {
				log.Printf("Error parsing INPUT: %v", err)
			}

		case "TIMEOUT":
			if d, err := parseDuration(value); err == nil {
				instr.Timeout = d
			}

		case "RETRY":
			var retry int
			if _, err := fmt.Sscanf(value, "%d", &retry); err == nil {
				instr.Retry = retry
			}

		case "WAIT":
			instr.Wait = strings.ToLower(value) == "true"

		case "INTERVAL":
			if d, err := parseDuration(value); err == nil {
				instr.Interval = d
			}

		case "TYPE":
			instr.QueryType = value

		case "NAME":
			instr.SignalName = value

		case "DATA":
			if err := parseJSONInput(value, instr.SignalData); err != nil {
				log.Printf("Error parsing DATA: %v", err)
			}

		case "WORKFLOW_ID":
			instr.WorkflowID = value

		case "RUN_ID":
			instr.RunID = value

		case "PRIORITY":
			var priority int
			if _, err := fmt.Sscanf(value, "%d", &priority); err == nil {
				instr.Priority = priority
			}

		case "IDEMPOTENCY_KEY":
			instr.IdempotencyKey = value

		case "DESCRIPTION":
			instr.Description = value
		}
	}

	return instr, nil
}

// parseJSONInput parses JSON value and merges it into target map
func parseJSONInput(jsonStr string, target map[string]interface{}) error {
	jsonStr = strings.TrimSpace(jsonStr)

	// Handle inline JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	// Merge into target
	for k, v := range data {
		target[k] = v
	}

	return nil
}

// parseDuration parses duration strings like "5m", "30s", "2h"
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	// Try standard Go duration parsing first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Try common short forms
	switch strings.ToLower(s) {
	case "immediate", "now":
		return 0, nil
	case "default":
		return 5 * time.Minute, nil
	default:
		// If it's a number, assume seconds
		var seconds int
		if _, err := fmt.Sscanf(s, "%d", &seconds); err == nil {
			return time.Duration(seconds) * time.Second, nil
		}
	}

	return 0, fmt.Errorf("invalid duration: %s", s)
}

// ValidateInstruction validates that a temporal instruction has required fields
func ValidateInstruction(instr TemporalInstruction) error {
	switch instr.Type {
	case InstructionTypeWorkflow:
		if instr.Name == "" {
			return fmt.Errorf("WORKFLOW instruction requires NAME")
		}

	case InstructionTypeSchedule:
		if instr.Name == "" {
			return fmt.Errorf("SCHEDULE instruction requires NAME")
		}
		if instr.Interval == 0 {
			return fmt.Errorf("SCHEDULE instruction requires INTERVAL")
		}

	case InstructionTypeQuery:
		if instr.WorkflowID == "" {
			return fmt.Errorf("QUERY instruction requires WORKFLOW_ID")
		}
		if instr.QueryType == "" {
			return fmt.Errorf("QUERY instruction requires TYPE")
		}

	case InstructionTypeSignal:
		if instr.WorkflowID == "" {
			return fmt.Errorf("SIGNAL instruction requires WORKFLOW_ID")
		}
		if instr.SignalName == "" {
			return fmt.Errorf("SIGNAL instruction requires NAME")
		}

	case InstructionTypeActivity:
		if instr.Name == "" {
			return fmt.Errorf("ACTIVITY instruction requires NAME")
		}

	case InstructionTypeCancelWF:
		if instr.WorkflowID == "" {
			return fmt.Errorf("CANCEL instruction requires WORKFLOW_ID")
		}

	case InstructionTypeListWF:
		// LIST has no required fields beyond type

	default:
		return fmt.Errorf("unknown instruction type: %s", instr.Type)
	}

	return nil
}

// FormatDSL creates a temporal DSL block from an instruction (for logging/debugging)
func FormatDSL(instr TemporalInstruction) string {
	var sb strings.Builder
	sb.WriteString("<temporal>\n")
	sb.WriteString(fmt.Sprintf("%s: %s\n", instr.Type, instr.Name))

	if instr.Timeout > 0 {
		sb.WriteString(fmt.Sprintf("  TIMEOUT: %v\n", instr.Timeout))
	}

	if instr.Retry > 0 {
		sb.WriteString(fmt.Sprintf("  RETRY: %d\n", instr.Retry))
	}

	if instr.Wait {
		sb.WriteString("  WAIT: true\n")
	}

	if instr.Interval > 0 {
		sb.WriteString(fmt.Sprintf("  INTERVAL: %v\n", instr.Interval))
	}

	if len(instr.Input) > 0 {
		data, _ := json.Marshal(instr.Input)
		sb.WriteString(fmt.Sprintf("  INPUT: %s\n", string(data)))
	}

	if instr.QueryType != "" {
		sb.WriteString(fmt.Sprintf("  TYPE: %s\n", instr.QueryType))
	}

	if instr.SignalName != "" {
		sb.WriteString(fmt.Sprintf("  NAME: %s\n", instr.SignalName))
	}

	if len(instr.SignalData) > 0 {
		data, _ := json.Marshal(instr.SignalData)
		sb.WriteString(fmt.Sprintf("  DATA: %s\n", string(data)))
	}

	sb.WriteString("END\n")
	sb.WriteString("</temporal>")

	return sb.String()
}
