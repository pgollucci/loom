// Bug Reporter - Headless UI Debugging System
// Captures full UI state and auto-files beads without screenshots

(function() {
    'use strict';

    // Track JavaScript errors
    window.__jsErrors = window.__jsErrors || [];
    window.__failedRequests = window.__failedRequests || [];
    window.__consoleLog = window.__consoleLog || [];

    // Capture JS errors
    window.addEventListener('error', function(e) {
        window.__jsErrors.push({
            message: e.message,
            source: e.filename,
            line: e.lineno,
            column: e.colno,
            stack: e.error?.stack,
            timestamp: new Date().toISOString()
        });
    });

    // Capture unhandled promise rejections
    window.addEventListener('unhandledrejection', function(e) {
        window.__jsErrors.push({
            message: 'Unhandled Promise Rejection: ' + e.reason,
            stack: e.reason?.stack,
            timestamp: new Date().toISOString()
        });
    });

    // Intercept fetch to track failed requests
    const originalFetch = window.fetch;
    window.fetch = function(...args) {
        const url = args[0];
        const startTime = Date.now();

        return originalFetch.apply(this, args).then(response => {
            const duration = Date.now() - startTime;

            if (!response.ok) {
                // Clone response to read body without consuming it
                response.clone().text().then(body => {
                    window.__failedRequests.push({
                        url: url,
                        status: response.status,
                        statusText: response.statusText,
                        duration: duration,
                        body: body.substring(0, 500), // First 500 chars
                        timestamp: new Date().toISOString()
                    });
                });
            }
            return response;
        }).catch(error => {
            window.__failedRequests.push({
                url: url,
                error: error.message,
                duration: Date.now() - startTime,
                timestamp: new Date().toISOString()
            });
            throw error;
        });
    };

    // Capture UI state
    function captureUIState() {
        // Find elements with "loading" text
        const loadingElements = Array.from(document.querySelectorAll('*')).filter(el => {
            return el.textContent && el.textContent.includes('Loading');
        }).map(el => ({
            tag: el.tagName,
            id: el.id,
            class: el.className,
            text: el.textContent.substring(0, 100)
        }));

        // Find error messages in DOM
        const errorElements = Array.from(document.querySelectorAll('.error, [class*="error"]')).map(el => ({
            tag: el.tagName,
            id: el.id,
            class: el.className,
            text: el.textContent.substring(0, 200),
            visible: el.offsetParent !== null
        }));

        return {
            url: window.location.href,
            timestamp: new Date().toISOString(),
            userAgent: navigator.userAgent,
            viewport: {
                width: window.innerWidth,
                height: window.innerHeight
            },
            dom: {
                title: document.title,
                bodyHTML: document.body ? document.body.outerHTML.substring(0, 50000) : '', // First 50KB
                activeElement: document.activeElement ? {
                    tag: document.activeElement.tagName,
                    id: document.activeElement.id,
                    class: document.activeElement.className
                } : null,
                loadingElements: loadingElements,
                errorElements: errorElements
            },
            javascript: {
                errors: window.__jsErrors || [],
                console: window.__consoleLog || []
            },
            network: {
                failed: window.__failedRequests || []
            },
            state: {
                hasLocalStorage: typeof localStorage !== 'undefined',
                hasSessionStorage: typeof sessionStorage !== 'undefined',
                cookieCount: document.cookie.split(';').length
            }
        };
    }

    // Create bug report UI
    function createBugReportUI() {
        // Check if already exists
        if (document.getElementById('bug-report-btn')) {
            return;
        }

        // Bug button
        const bugBtn = document.createElement('button');
        bugBtn.id = 'bug-report-btn';
        bugBtn.innerHTML = '🐛';
        bugBtn.title = 'Report Bug (Captures full page state)';
        bugBtn.style.cssText = `
            position: fixed;
            bottom: 20px;
            right: 20px;
            z-index: 9999;
            background: #ff4444;
            color: white;
            border: none;
            border-radius: 50%;
            width: 60px;
            height: 60px;
            font-size: 24px;
            cursor: pointer;
            box-shadow: 0 4px 8px rgba(0,0,0,0.3);
            transition: transform 0.2s;
        `;
        bugBtn.onmouseenter = function() { this.style.transform = 'scale(1.1)'; };
        bugBtn.onmouseleave = function() { this.style.transform = 'scale(1)'; };

        // Modal
        const modal = document.createElement('div');
        modal.id = 'bug-report-modal';
        modal.style.cssText = `
            display: none;
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0,0,0,0.5);
            z-index: 10000;
            align-items: center;
            justify-content: center;
        `;

        modal.innerHTML = `
            <div style="background: white; padding: 30px; border-radius: 8px; max-width: 500px; width: 90%; box-shadow: 0 4px 16px rgba(0,0,0,0.3);">
                <h2 style="margin: 0 0 15px 0; color: #333;">🐛 Report Bug</h2>
                <p style="margin: 0 0 15px 0; color: #666;">Describe the issue you're experiencing:</p>
                <input type="text" id="bug-description" placeholder="e.g., Workflows page shows no data"
                    style="width: 100%; padding: 10px; margin: 10px 0; border: 1px solid #ccc; border-radius: 4px; font-size: 14px; box-sizing: border-box;">
                <div style="display: flex; gap: 10px; margin-top: 20px;">
                    <button id="submit-bug-btn" style="flex: 1; padding: 12px; background: #4CAF50; color: white;
                        border: none; border-radius: 4px; cursor: pointer; font-weight: 600;">Submit Bug Report</button>
                    <button id="cancel-bug-btn" style="flex: 1; padding: 12px; background: #ccc; color: black;
                        border: none; border-radius: 4px; cursor: pointer; font-weight: 600;">Cancel</button>
                </div>
                <p style="margin-top: 15px; font-size: 12px; color: #999; line-height: 1.5;">
                    ℹ️ This will capture:
                    • Full page HTML
                    • Console errors
                    • Failed network requests
                    • Page state<br>
                    No personal data is collected.
                </p>
            </div>
        `;

        document.body.appendChild(bugBtn);
        document.body.appendChild(modal);

        // Event handlers
        bugBtn.addEventListener('click', function() {
            modal.style.display = 'flex';
            document.getElementById('bug-description').focus();
        });

        document.getElementById('cancel-bug-btn').addEventListener('click', function() {
            modal.style.display = 'none';
            document.getElementById('bug-description').value = '';
        });

        document.getElementById('submit-bug-btn').addEventListener('click', async function() {
            const description = document.getElementById('bug-description').value.trim();
            if (!description) {
                alert('Please enter a bug description');
                return;
            }

            const submitBtn = this;
            submitBtn.disabled = true;
            submitBtn.textContent = 'Capturing UI state...';

            try {
                const capture = captureUIState();
                capture.description = description;

                const response = await fetch('/api/v1/debug/capture-ui', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify(capture)
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error('Server error: ' + errorText);
                }

                const result = await response.json();
                alert('✅ Bug report filed successfully!\\n\\nBead ID: ' + result.bead_id + '\\n\\nAn agent will investigate shortly.');
                modal.style.display = 'none';
                document.getElementById('bug-description').value = '';

                // Clear captured errors after successful report
                window.__jsErrors = [];
                window.__failedRequests = [];
            } catch (error) {
                alert('❌ Failed to submit bug report:\\n' + error.message + '\\n\\nPlease try again or contact support.');
                console.error('Bug report submission failed:', error);
            } finally {
                submitBtn.disabled = false;
                submitBtn.textContent = 'Submit Bug Report';
            }
        });

        // Allow Enter key to submit
        document.getElementById('bug-description').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                document.getElementById('submit-bug-btn').click();
            }
        });

        // Close modal on Escape
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape' && modal.style.display === 'flex') {
                modal.style.display = 'none';
                document.getElementById('bug-description').value = '';
            }
        });
    }

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', createBugReportUI);
    } else {
        createBugReportUI();
    }

    console.log('🐛 Bug Reporter initialized - Click the bug button to report issues');
})();
