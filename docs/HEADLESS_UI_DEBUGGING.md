# Headless UI Debugging Strategy

## Problem Statement

When running on remote SSH systems without browser access, we need the ability to:
1. Capture UI state without screenshots
2. Debug "no data" issues remotely
3. Allow users to report bugs with full context via a simple UI button

## Current Issue: workflows.html

**Diagnosis (captured headlessly):**
```bash
# Fetch page
curl -s http://sparky.local:8080/static/workflows.html

# Check API endpoint
curl -s http://sparky.local:8080/api/v1/workflows
# Returns: "Failed to list workflows: no such table: workflows"
```

**Root Cause:** Database migration missing `workflows` table

## Solution 1: DOM Capture Tool

### Immediate Implementation

Create a JavaScript snippet that captures full DOM state:

```javascript
// Add to web/static/js/debug-capture.js
function captureUIState() {
    return {
        url: window.location.href,
        timestamp: new Date().toISOString(),
        userAgent: navigator.userAgent,
        viewport: {
            width: window.innerWidth,
            height: window.innerHeight
        },
        dom: {
            html: document.documentElement.outerHTML,
            activeElement: document.activeElement?.tagName,
            focusedElement: document.activeElement?.outerHTML
        },
        javascript: {
            errors: window.__jsErrors || [],
            console: window.__consoleLog || []
        },
        network: {
            failed: window.__failedRequests || [],
            pending: window.__pendingRequests || []
        },
        state: {
            localStorage: {...localStorage},
            sessionStorage: {...sessionStorage},
            cookies: document.cookie
        }
    };
}
```

### Backend Capture Endpoint

```go
// Add to internal/api/handlers_debug.go
func (s *Server) handleCaptureUIState(w http.ResponseWriter, r *http.Request) {
    var capture struct {
        URL         string                 `json:"url"`
        Timestamp   string                 `json:"timestamp"`
        UserAgent   string                 `json:"user_agent"`
        DOM         map[string]interface{} `json:"dom"`
        JavaScript  map[string]interface{} `json:"javascript"`
        Network     map[string]interface{} `json:"network"`
        State       map[string]interface{} `json:"state"`
        Description string                 `json:"description"` // User's one-line bug description
    }

    if err := json.NewDecoder(r.Body).Decode(&capture); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    // Auto-file as bead with full context
    title := fmt.Sprintf("[UI Bug] %s", capture.Description)
    description := fmt.Sprintf(`User-reported UI bug

## Description
%s

## URL
%s

## Timestamp
%s

## Browser
%s

## Failed Network Requests
%v

## JavaScript Errors
%v

## DOM Snapshot
Available in bead context
`, capture.Description, capture.URL, capture.Timestamp, capture.UserAgent,
        capture.Network["failed"], capture.JavaScript["errors"])

    bead, err := s.app.CreateBead(title, description, models.P0, "bug", "loom-self")
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Store full DOM in bead context
    s.app.UpdateBeadContext(bead.ID, "ui_capture", marshal(capture))

    json.NewEncoder(w).Encode(map[string]string{"bead_id": bead.ID})
}
```

## Solution 2: "Report Bug" Button

### UI Component

Add to every page (in web/static/index.html and all pages):

```html
<!-- Bug Report Button (fixed position) -->
<button id="bug-report-btn" style="position: fixed; bottom: 20px; right: 20px; z-index: 9999;
    background: #ff4444; color: white; border: none; border-radius: 50%; width: 60px; height: 60px;
    font-size: 24px; cursor: pointer; box-shadow: 0 4px 8px rgba(0,0,0,0.3);">
    🐛
</button>

<div id="bug-report-modal" style="display: none; position: fixed; top: 0; left: 0; right: 0; bottom: 0;
    background: rgba(0,0,0,0.5); z-index: 10000; align-items: center; justify-content: center;">
    <div style="background: white; padding: 30px; border-radius: 8px; max-width: 500px; width: 90%;">
        <h2>Report Bug</h2>
        <p>Describe the issue you're experiencing:</p>
        <input type="text" id="bug-description" placeholder="e.g., Workflows page shows no data"
            style="width: 100%; padding: 10px; margin: 10px 0; border: 1px solid #ccc; border-radius: 4px;">
        <div style="display: flex; gap: 10px; margin-top: 20px;">
            <button id="submit-bug-btn" style="flex: 1; padding: 10px; background: #4CAF50; color: white;
                border: none; border-radius: 4px; cursor: pointer;">Submit Bug Report</button>
            <button id="cancel-bug-btn" style="flex: 1; padding: 10px; background: #ccc; color: black;
                border: none; border-radius: 4px; cursor: pointer;">Cancel</button>
        </div>
        <p style="margin-top: 15px; font-size: 12px; color: #666;">
            This will capture the full page state, console errors, network failures, and create a bead for investigation.
        </p>
    </div>
</div>

<script src="/static/js/bug-reporter.js"></script>
```

### JavaScript Handler

```javascript
// web/static/js/bug-reporter.js
(function() {
    // Capture JavaScript errors
    window.__jsErrors = [];
    window.addEventListener('error', function(e) {
        window.__jsErrors.push({
            message: e.message,
            source: e.filename,
            line: e.lineno,
            column: e.colno,
            stack: e.error?.stack
        });
    });

    // Capture failed network requests
    window.__failedRequests = [];
    const originalFetch = window.fetch;
    window.fetch = function(...args) {
        return originalFetch.apply(this, args).then(response => {
            if (!response.ok) {
                window.__failedRequests.push({
                    url: args[0],
                    status: response.status,
                    statusText: response.statusText
                });
            }
            return response;
        }).catch(error => {
            window.__failedRequests.push({
                url: args[0],
                error: error.message
            });
            throw error;
        });
    };

    // Bug report modal
    const bugBtn = document.getElementById('bug-report-btn');
    const bugModal = document.getElementById('bug-report-modal');
    const bugDescription = document.getElementById('bug-description');
    const submitBtn = document.getElementById('submit-bug-btn');
    const cancelBtn = document.getElementById('cancel-bug-btn');

    bugBtn.addEventListener('click', () => {
        bugModal.style.display = 'flex';
        bugDescription.focus();
    });

    cancelBtn.addEventListener('click', () => {
        bugModal.style.display = 'none';
        bugDescription.value = '';
    });

    submitBtn.addEventListener('click', async () => {
        const description = bugDescription.value.trim();
        if (!description) {
            alert('Please enter a bug description');
            return;
        }

        submitBtn.disabled = true;
        submitBtn.textContent = 'Capturing...';

        try {
            const capture = captureUIState();
            capture.description = description;

            const response = await fetch('/api/v1/debug/capture-ui', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(capture)
            });

            if (!response.ok) throw new Error('Failed to submit bug report');

            const result = await response.json();
            alert(`Bug report filed! Bead ID: ${result.bead_id}`);
            bugModal.style.display = 'none';
            bugDescription.value = '';
        } catch (error) {
            alert('Failed to submit bug report: ' + error.message);
        } finally {
            submitBtn.disabled = false;
            submitBtn.textContent = 'Submit Bug Report';
        }
    });

    function captureUIState() {
        // ... (implementation from above)
    }
})();
```

## Solution 3: CLI Tool for Headless Debugging

```bash
#!/bin/bash
# scripts/debug-ui.sh - Debug UI issues headlessly

URL="${1:-http://localhost:8080}"
PAGE="${2:-/}"

echo "=== Headless UI Debugger ==="
echo "Target: $URL$PAGE"
echo

# 1. Fetch HTML
echo "[1/5] Fetching HTML..."
HTML=$(curl -s "$URL$PAGE")
echo "✓ HTML size: $(echo "$HTML" | wc -c) bytes"

# 2. Extract JavaScript sources
echo "[2/5] Extracting JavaScript sources..."
curl -s "$URL$PAGE" | grep -oP 'src="[^"]*\.js"' | sed 's/src="//;s/"//' | while read js; do
    echo "  - $js"
done

# 3. Test API endpoints
echo "[3/5] Testing API endpoints..."
curl -s "$URL$PAGE" | grep -oP "fetch\(['\"][^'\"]*['\"]" | sed "s/fetch(['\"]//;s/['\"].*//" | while read api; do
    if [[ $api == /* ]]; then
        full_url="$URL$api"
    else
        full_url="$api"
    fi
    status=$(curl -s -o /dev/null -w "%{http_code}" "$full_url")
    response=$(curl -s "$full_url" | head -c 200)
    echo "  [$status] $api"
    if [[ $status != 200 ]]; then
        echo "       Error: $response"
    fi
done

# 4. Check for JavaScript errors in console
echo "[4/5] Checking for obvious issues..."
if echo "$HTML" | grep -q "Loading.*\.\.\."; then
    echo "  ⚠ Found 'Loading...' text (possible stuck loading state)"
fi

# 5. Capture full state
echo "[5/5] Capturing full state..."
OUTPUT_FILE="/tmp/ui-debug-$(date +%s).json"
cat > "$OUTPUT_FILE" <<EOF
{
  "url": "$URL$PAGE",
  "timestamp": "$(date -Iseconds)",
  "html_size": $(echo "$HTML" | wc -c),
  "has_loading_indicator": $(echo "$HTML" | grep -q "Loading" && echo "true" || echo "false"),
  "api_checks": []
}
EOF

echo "✓ Full state saved to: $OUTPUT_FILE"
echo
echo "=== Summary ==="
echo "Run this on the server to debug UI issues without browser access"
```

## Implementation Priority

1. **Immediate (today):**
   - Fix workflows table migration
   - Add headless debug script

2. **This week:**
   - Implement bug report button
   - Add `/api/v1/debug/capture-ui` endpoint

3. **Next sprint:**
   - Automated UI testing with headless Chrome
   - Screenshot capture via Puppeteer (when available)
   - DOM diff comparison for regression testing

## Usage Examples

### Debugging Remotely

```bash
# On SSH server
curl -s http://localhost:8080/static/workflows.html | grep -A 5 "class=\"loading\""
curl -s http://localhost:8080/api/v1/workflows
# Returns: "Failed to list workflows: no such table: workflows"
# → Diagnosed: Database migration needed

# Use debug script
./scripts/debug-ui.sh http://localhost:8080 /static/workflows.html
```

### User Reports Bug

1. User clicks 🐛 button
2. Types: "Workflows page shows no data"
3. System captures:
   - Full DOM
   - API error: "no such table: workflows"
   - Failed fetch to /api/v1/workflows (500)
4. Auto-files bead: "[UI Bug] Workflows page shows no data"
5. Agent investigates with full context

## Benefits

✅ **No screenshots needed** - DOM + API errors = complete picture
✅ **User-friendly** - One-click bug reporting
✅ **Full context** - Agents get everything needed to fix
✅ **Remote-friendly** - Works over SSH
✅ **Automated** - Reduces back-and-forth debugging

## References

- Puppeteer (headless Chrome): https://pptr.dev/
- Playwright (cross-browser testing): https://playwright.dev/
- DOM serialization: https://developer.mozilla.org/en-US/docs/Web/API/XMLSerializer
