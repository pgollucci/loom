#!/usr/bin/env python3
import re

with open('internal/loom/loom_lifecycle.go', 'r') as f:
    lines = f.readlines()

# Lines to fix (0-indexed): 444, 467, 557, 686
fix_lines = [444, 467, 557, 686]

for line_num in fix_lines:
    if line_num < len(lines):
        line = lines[line_num]
        indent = len(line) - len(line.lstrip())
        spaces = ' ' * indent
        
        # Extract the function call
        match = re.search(r'_ = (a\.database\.UpsertProject\([^)]+\))', line)
        if match:
            call = match.group(1)
            # Determine variable name from the call
            if '&p' in call:
                var = 'p'
            elif 'proj' in call:
                var = 'proj'
            else:
                var = 'p'
            
            # Replace with error handling
            new_line = f"{spaces}if err := {call}; err != nil {{\n{spaces}\tlog.Printf(\"[Loom] Warning: failed to persist project %s to database: %v\", {var}.ID, err)\n{spaces}}}\n"
            lines[line_num] = new_line

with open('internal/loom/loom_lifecycle.go', 'w') as f:
    f.writelines(lines)

print('Fixed error handling in loom_lifecycle.go')
