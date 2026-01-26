// AgentiCorp UI Error Handler and Auto-Filer
// Catches ALL JS errors and auto-files them as beads

(function() {
    'use strict';
    
    // Track errors to avoid duplicate filings
    const filedErrors = new Set();
    const ERROR_DEBOUNCE_MS = 5000; // Don't file same error within 5 seconds
    
    // Show toast notification (uses app.js showToast if available, falls back to console)
    function showErrorToast(message, type = 'error') {
        if (typeof showToast === 'function') {
            showToast(message, type);
        } else {
            console.log(`[Toast ${type}] ${message}`);
        }
    }
    
    // Auto-file error to backend
    async function autoFileError(errorInfo) {
        // Create a unique key for deduplication
        const errorKey = `${errorInfo.message}:${errorInfo.source}:${errorInfo.lineno}`;
        
        // Check if we've already filed this error recently
        if (filedErrors.has(errorKey)) {
            return;
        }
        filedErrors.add(errorKey);
        
        // Clear from set after debounce period
        setTimeout(() => filedErrors.delete(errorKey), ERROR_DEBOUNCE_MS);
        
        // Show immediate toast
        showErrorToast('Internal UI error detected', 'error');
        
        const bugReport = {
            title: `[auto-filed] UI Error: ${(errorInfo.message || 'Unknown error').substring(0, 80)}`,
            source: 'frontend',
            error_type: 'js_error',
            message: errorInfo.message || 'Unknown error',
            stack_trace: errorInfo.stack || '',
            context: {
                url: window.location.href,
                source_file: errorInfo.source || 'unknown',
                line: errorInfo.lineno || 0,
                column: errorInfo.colno || 0,
                user_agent: navigator.userAgent,
                timestamp: new Date().toISOString(),
                viewport: `${window.innerWidth}x${window.innerHeight}`,
                state: typeof state !== 'undefined' ? {
                    beads: state.beads?.length || 0,
                    projects: state.projects?.length || 0,
                    agents: state.agents?.length || 0,
                    providers: state.providers?.length || 0
                } : 'state not available'
            },
            severity: 'high',
            occurred_at: new Date().toISOString()
        };
        
        try {
            const response = await fetch('/api/v1/beads/auto-file', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(bugReport)
            });
            
            if (response.ok) {
                const result = await response.json();
                showErrorToast(`Bug filed: ${result.bead_id}`, 'success');
                console.log(`[AutoFile] Bug report filed: ${result.bead_id}, assigned to: ${result.assigned_to}`);
            } else {
                const errorText = await response.text();
                showErrorToast('Failed to file bug report', 'error');
                console.error('[AutoFile] Failed to file bug report:', errorText);
            }
        } catch (error) {
            showErrorToast('Cannot reach backend to file bug', 'error');
            console.error('[AutoFile] Error filing bug report:', error);
        }
    }
    
    // Global error handler for uncaught errors
    window.onerror = function(message, source, lineno, colno, error) {
        console.error('[GlobalError]', message, 'at', source, lineno, colno);
        
        autoFileError({
            message: message,
            source: source,
            lineno: lineno,
            colno: colno,
            stack: error?.stack || ''
        });
        
        // Don't prevent default error handling
        return false;
    };
    
    // Global handler for unhandled promise rejections
    window.addEventListener('unhandledrejection', function(event) {
        const error = event.reason;
        const message = error?.message || String(error) || 'Unhandled promise rejection';
        
        console.error('[UnhandledRejection]', message);
        
        autoFileError({
            message: `Promise rejection: ${message}`,
            source: 'promise',
            lineno: 0,
            colno: 0,
            stack: error?.stack || ''
        });
    });
    
    // Intercept console.error to catch errors logged but not thrown
    const originalConsoleError = console.error;
    console.error = function(...args) {
        // Call original first
        originalConsoleError.apply(console, args);
        
        // Check if this looks like an actual error (not just a log)
        const errorMsg = args.map(a => String(a)).join(' ');
        
        // Filter out expected/handled errors to avoid noise
        const isExpectedError = 
            errorMsg.includes('[GlobalError]') ||       // Already handled by onerror
            errorMsg.includes('[UnhandledRejection]') || // Already handled
            errorMsg.includes('[AutoFile]') ||          // Our own logging
            errorMsg.includes('[AgentiCorp]') ||        // App's own error handling
            errorMsg.includes('Failed to load');        // Normal load failures shown in toast
        
        // File if it contains error indicators and isn't expected
        const looksLikeError = 
            errorMsg.includes('Error') ||
            errorMsg.includes('error') ||
            errorMsg.includes('Cannot read') ||
            errorMsg.includes('undefined') ||
            errorMsg.includes('is not a function') ||
            errorMsg.includes('is not defined');
        
        if (looksLikeError && !isExpectedError) {
            autoFileError({
                message: errorMsg.substring(0, 200),
                source: 'console.error',
                lineno: 0,
                colno: 0,
                stack: ''
            });
        }
    };
    
    // Manual filing function available globally
    window.fileUIBug = function(message) {
        autoFileError({
            message: message || 'Manually reported UI issue',
            source: 'manual',
            lineno: 0,
            colno: 0,
            stack: new Error().stack
        });
    };
    
    console.log('[Diagnostic] UI error auto-filer initialized');
    
})();
