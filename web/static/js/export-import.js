// Export/Import functionality for Loom database

// Initialize export/import handlers when DOM is ready
document.addEventListener('DOMContentLoaded', function() {
    initializeExportImport();
});

function initializeExportImport() {
    // Load projects for export filter
    loadProjectsForExport();

    // Export form handler
    const exportForm = document.getElementById('export-form');
    if (exportForm) {
        exportForm.addEventListener('submit', handleExport);
    }

    // Import form handler
    const importForm = document.getElementById('import-form');
    if (importForm) {
        importForm.addEventListener('submit', handleImport);
    }

    // Handle dry-run checkbox (disable validate-only when checked)
    const dryRunCheckbox = document.getElementById('import-dry-run');
    const validateOnlyCheckbox = document.getElementById('import-validate-only');

    if (dryRunCheckbox && validateOnlyCheckbox) {
        dryRunCheckbox.addEventListener('change', function() {
            if (this.checked) {
                validateOnlyCheckbox.checked = false;
            }
        });

        validateOnlyCheckbox.addEventListener('change', function() {
            if (this.checked) {
                dryRunCheckbox.checked = false;
            }
        });
    }
}

async function loadProjectsForExport() {
    try {
        const response = await fetch('/api/v1/projects');
        if (!response.ok) throw new Error('Failed to load projects');

        const projects = await response.json();
        const select = document.getElementById('export-project');

        if (select && Array.isArray(projects)) {
            // Clear existing options except "All Projects"
            while (select.options.length > 1) {
                select.remove(1);
            }

            // Add project options
            projects.forEach(project => {
                const option = document.createElement('option');
                option.value = project.id;
                option.textContent = `${project.name} (${project.id})`;
                select.appendChild(option);
            });
        }
    } catch (error) {
        console.error('Error loading projects:', error);
    }
}

async function handleExport(event) {
    event.preventDefault();

    const statusDiv = document.getElementById('export-status');
    statusDiv.style.display = 'block';
    statusDiv.style.background = '#3b82f6';
    statusDiv.style.color = 'white';
    statusDiv.textContent = '⏳ Preparing export...';

    try {
        // Build query parameters
        const params = new URLSearchParams();

        const format = document.getElementById('export-format').value;
        if (format) params.append('format', format);

        const include = document.getElementById('export-include').value.trim();
        if (include) params.append('include', include);

        const projectId = document.getElementById('export-project').value;
        if (projectId) params.append('project_id', projectId);

        const since = document.getElementById('export-since').value;
        if (since) {
            // Convert datetime-local to RFC3339
            const date = new Date(since);
            params.append('since', date.toISOString());
        }

        // Make request
        const url = `/api/v1/export?${params.toString()}`;
        const response = await fetch(url);

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(`Export failed: ${errorText}`);
        }

        // Get filename from Content-Disposition header or use default
        const contentDisposition = response.headers.get('Content-Disposition');
        let filename = 'loom-export.json';
        if (contentDisposition) {
            const matches = /filename[^;=\n]*=((['"]).*?\2|[^;\n]*)/.exec(contentDisposition);
            if (matches && matches[1]) {
                filename = matches[1].replace(/['"]/g, '');
            }
        }

        // Download file
        const blob = await response.blob();
        const downloadUrl = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = downloadUrl;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        window.URL.revokeObjectURL(downloadUrl);
        document.body.removeChild(a);

        // Show success
        statusDiv.style.background = '#10b981';
        statusDiv.textContent = `✅ Export downloaded: ${filename}`;

        // Parse the blob to show stats
        try {
            const text = await blob.text();
            const data = JSON.parse(text);
            if (data.export_metadata && data.export_metadata.record_counts) {
                const counts = data.export_metadata.record_counts;
                const total = Object.values(counts).reduce((sum, count) => sum + count, 0);
                statusDiv.textContent += ` (${total} records across ${Object.keys(counts).length} tables)`;
            }
        } catch (e) {
            // Ignore parse errors for stats
        }

    } catch (error) {
        console.error('Export error:', error);
        statusDiv.style.background = '#ef4444';
        statusDiv.textContent = `❌ Export failed: ${error.message}`;
    }
}

async function handleImport(event) {
    event.preventDefault();

    const statusDiv = document.getElementById('import-status');
    const summaryDiv = document.getElementById('import-summary');
    const summaryContent = document.getElementById('import-summary-content');

    statusDiv.style.display = 'block';
    statusDiv.style.background = '#3b82f6';
    statusDiv.style.color = 'white';
    statusDiv.textContent = '⏳ Reading import file...';
    summaryDiv.style.display = 'none';

    try {
        // Get file
        const fileInput = document.getElementById('import-file');
        if (!fileInput.files || fileInput.files.length === 0) {
            throw new Error('Please select a file to import');
        }

        const file = fileInput.files[0];

        // Validate file size (50MB limit)
        const maxSize = 50 * 1024 * 1024;
        if (file.size > maxSize) {
            throw new Error(`File too large (${(file.size / 1024 / 1024).toFixed(2)}MB). Maximum size is 50MB.`);
        }

        statusDiv.textContent = '⏳ Uploading and validating...';

        // Read file
        const fileContent = await file.text();

        // Validate JSON
        let importData;
        try {
            importData = JSON.parse(fileContent);
        } catch (e) {
            throw new Error('Invalid JSON file');
        }

        // Build query parameters
        const params = new URLSearchParams();

        const strategy = document.getElementById('import-strategy').value;
        params.append('strategy', strategy);

        const dryRun = document.getElementById('import-dry-run').checked;
        if (dryRun) params.append('dry_run', 'true');

        const validateOnly = document.getElementById('import-validate-only').checked;
        if (validateOnly) params.append('validate_only', 'true');

        // Confirm replace strategy
        if (strategy === 'replace' && !dryRun && !validateOnly) {
            if (!confirm('⚠️ REPLACE strategy will DELETE ALL EXISTING DATA and replace it with the import. This cannot be undone. Are you sure?')) {
                statusDiv.style.background = '#6b7280';
                statusDiv.textContent = '⏸️ Import cancelled';
                return;
            }
        }

        statusDiv.textContent = dryRun ? '⏳ Running dry-run import...' :
                                validateOnly ? '⏳ Validating import...' :
                                '⏳ Importing data...';

        // Make request
        const url = `/api/v1/import?${params.toString()}`;
        const response = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: fileContent
        });

        const result = await response.json();

        if (!response.ok) {
            throw new Error(result.error || 'Import failed');
        }

        // Show result
        if (validateOnly) {
            if (result.validation && result.validation.schema_version_ok) {
                statusDiv.style.background = '#10b981';
                statusDiv.textContent = '✅ Validation successful - file can be imported';
            } else {
                statusDiv.style.background = '#f59e0b';
                statusDiv.textContent = `⚠️ Validation warning: ${result.validation.validation_message || 'Schema version mismatch'}`;
            }
        } else if (dryRun) {
            statusDiv.style.background = '#10b981';
            statusDiv.textContent = '✅ Dry run completed - no changes were made';
        } else {
            if (result.status === 'completed') {
                statusDiv.style.background = '#10b981';
                statusDiv.textContent = '✅ Import completed successfully';
            } else {
                statusDiv.style.background = '#f59e0b';
                statusDiv.textContent = `⚠️ Import completed with warnings`;
            }
        }

        // Show summary
        if (result.summary) {
            summaryDiv.style.display = 'block';
            summaryContent.innerHTML = buildImportSummaryHTML(result);
        }

        // Refresh UI if import was successful and not a dry run
        if (!dryRun && !validateOnly && result.status === 'completed') {
            statusDiv.textContent += ' - Refreshing UI...';
            setTimeout(() => {
                window.location.reload();
            }, 2000);
        }

    } catch (error) {
        console.error('Import error:', error);
        statusDiv.style.background = '#ef4444';
        statusDiv.textContent = `❌ Import failed: ${error.message}`;
        summaryDiv.style.display = 'none';
    }
}

function buildImportSummaryHTML(result) {
    let html = '<div style="display: grid; gap: 1rem;">';

    // Validation results
    if (result.validation) {
        html += '<div style="padding: 0.75rem; border-left: 3px solid ' +
                (result.validation.schema_version_ok ? '#10b981' : '#f59e0b') +
                '; background: rgba(0,0,0,0.02);">';
        html += '<strong>Validation:</strong><br>';
        html += `Schema Version: ${result.validation.schema_version_ok ? '✅ OK' : '⚠️ Mismatch'}<br>`;
        html += `Encryption Key: ${result.validation.encryption_key_ok ? '✅ OK' : '⚠️ Mismatch'}`;
        if (result.validation.validation_message) {
            html += `<br><span style="color: #f59e0b;">${result.validation.validation_message}</span>`;
        }
        html += '</div>';
    }

    // Table summary
    if (result.summary && Object.keys(result.summary).length > 0) {
        html += '<div>';
        html += '<strong>Import Summary:</strong>';
        html += '<table style="width: 100%; margin-top: 0.5rem; border-collapse: collapse;">';
        html += '<thead><tr style="background: rgba(0,0,0,0.05); text-align: left;">';
        html += '<th style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">Table</th>';
        html += '<th style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">Inserted</th>';
        html += '<th style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">Updated</th>';
        html += '<th style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">Skipped</th>';
        html += '<th style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">Failed</th>';
        html += '</tr></thead><tbody>';

        for (const [table, stats] of Object.entries(result.summary)) {
            const hasFailures = stats.failed > 0;
            const rowStyle = hasFailures ? 'background: rgba(239, 68, 68, 0.1);' : '';
            html += `<tr style="${rowStyle}">`;
            html += `<td style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);"><code>${table}</code></td>`;
            html += `<td style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">${stats.inserted || 0}</td>`;
            html += `<td style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">${stats.updated || 0}</td>`;
            html += `<td style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">${stats.skipped || 0}</td>`;
            html += `<td style="padding: 0.5rem; border-bottom: 1px solid var(--border-color);">`;
            html += hasFailures ? `<span style="color: #ef4444; font-weight: 600;">${stats.failed}</span>` : '0';
            html += `</td>`;
            html += '</tr>';
        }

        html += '</tbody></table>';
        html += '</div>';
    }

    // Errors
    if (result.errors && result.errors.length > 0) {
        html += '<div style="padding: 0.75rem; border-left: 3px solid #ef4444; background: rgba(239, 68, 68, 0.05);">';
        html += '<strong style="color: #ef4444;">Errors:</strong><br>';
        html += '<ul style="margin: 0.5rem 0 0 1.5rem; padding: 0;">';
        result.errors.forEach(error => {
            html += `<li style="color: #ef4444; font-size: 0.9rem;">${error}</li>`;
        });
        html += '</ul>';
        html += '</div>';
    }

    html += '</div>';
    return html;
}
