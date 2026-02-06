// Create Bead Modal

async function showCreateBeadModal() {
    const projects = await apiCall('/projects');
    const projectOptions = projects.map(p => ({
        value: p.id,
        label: p.name
    }));

    const res = await formModal({
        title: 'Create New Bead',
        submitText: 'Create Bead',
        fields: [
            {
                id: 'project_id',
                label: 'Project',
                type: 'select',
                required: true,
                options: projectOptions
            },
            {
                id: 'title',
                label: 'Title',
                type: 'text',
                required: true,
                placeholder: 'Brief description of the work'
            },
            {
                id: 'type',
                label: 'Type',
                type: 'select',
                required: true,
                value: 'task',
                options: [
                    { value: 'task', label: 'Task' },
                    { value: 'bug', label: 'Bug' },
                    { value: 'feature', label: 'Feature' },
                    { value: 'epic', label: 'Epic' },
                    { value: 'decision', label: 'Decision' }
                ]
            },
            {
                id: 'priority',
                label: 'Priority',
                type: 'select',
                required: true,
                value: '2',
                options: [
                    { value: '0', label: 'P0 - Critical' },
                    { value: '1', label: 'P1 - High' },
                    { value: '2', label: 'P2 - Medium' },
                    { value: '3', label: 'P3 - Low' },
                    { value: '4', label: 'P4 - Backlog' }
                ]
            },
            {
                id: 'description',
                label: 'Description',
                type: 'textarea',
                required: true,
                rows: 8,
                placeholder: 'Detailed description of the work to be done...'
            },
            {
                id: 'assignee',
                label: 'Assignee (optional)',
                type: 'text',
                placeholder: 'agent-id or role name'
            },
            {
                id: 'tags',
                label: 'Tags (comma-separated)',
                type: 'text',
                placeholder: 'frontend, urgent, refactor'
            }
        ]
    });

    if (!res) return;

    try {
        // Parse tags
        const tags = res.tags ? res.tags.split(',').map(t => t.trim()).filter(t => t) : [];

        const bead = await apiCall('/beads', {
            method: 'POST',
            body: JSON.stringify({
                project_id: res.project_id,
                title: res.title,
                description: res.description,
                type: res.type,
                priority: parseInt(res.priority),
                assignee: res.assignee || undefined,
                tags: tags
            })
        });

        showToast('Bead created successfully', 'success');

        // Refresh views
        if (typeof loadBeads === 'function') {
            await loadBeads();
        }
        if (typeof render === 'function') {
            render();
        }

        return bead;
    } catch (error) {
        showToast(`Failed to create bead: ${error.message}`, 'error');
    }
}

// Quick bead creation from current context
async function quickCreateBead(projectId, title, type = 'task') {
    const description = prompt(`Enter description for ${type}:`, '');
    if (!description) return;

    try {
        const bead = await apiCall('/beads', {
            method: 'POST',
            body: JSON.stringify({
                project_id: projectId,
                title: title,
                description: description,
                type: type,
                priority: 2
            })
        });

        showToast('Bead created', 'success');

        if (typeof loadBeads === 'function') {
            await loadBeads();
        }
        if (typeof render === 'function') {
            render();
        }

        return bead;
    } catch (error) {
        showToast(`Failed to create bead: ${error.message}`, 'error');
    }
}
