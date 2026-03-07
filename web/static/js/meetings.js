// Meeting Rooms Management

// Load active meetings from the backend
async function loadActiveMeetings() {
    try {
        state.activeMeetings = await apiCall('/meetings/active');
    } catch (error) {
        console.error('[Loom] Failed to load active meetings:', error);
        state.activeMeetings = [];
    }
}

// Render the active meetings panel
function renderActiveMeetings() {
    const container = document.getElementById('active-meetings-list');
    if (!container) return;

    if (!state.activeMeetings || state.activeMeetings.length === 0) {
        container.innerHTML = renderEmptyState('No active meetings', 'Meetings will appear here when agents are in discussion.');
        return;
    }

    container.innerHTML = state.activeMeetings.map(meeting => `
        <div class="meeting-card" style="padding: 1rem; border: 1px solid var(--border-color); border-radius: 4px; margin-bottom: 0.75rem; background: var(--bg-secondary);">
            <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 0.5rem;">
                <div>
                    <h4 style="margin: 0 0 0.25rem 0; color: var(--primary-color);">${escapeHtml(meeting.title || 'Untitled Meeting')}</h4>
                    <p class="small" style="margin: 0; color: var(--text-muted);">${escapeHtml(meeting.id)}</p>
                </div>
                <span class="badge" style="background: #16a34a; color: white;">${escapeHtml(meeting.status || 'active')}</span>
            </div>
            <div style="margin-bottom: 0.75rem;">
                <p class="small" style="margin: 0.25rem 0; color: var(--text-muted);"><strong>Started:</strong> ${formatDate(meeting.started_at)}</p>
                <p class="small" style="margin: 0.25rem 0; color: var(--text-muted);"><strong>Participants:</strong> ${meeting.participants ? meeting.participants.length : 0}</p>
            </div>
            <div style="margin-bottom: 0.75rem;">
                <p class="small" style="margin: 0.25rem 0;"><strong>Participants:</strong></p>
                <div style="display: flex; flex-wrap: wrap; gap: 0.5rem;">
                    ${(meeting.participants || []).map(p => `
                        <span class="badge" style="background: #3b82f6; color: white;">${escapeHtml(p)}</span>
                    `).join('')}
                </div>
            </div>
            ${meeting.description ? `<p class="small" style="margin: 0.5rem 0 0 0; color: var(--text-muted);">${escapeHtml(meeting.description)}</p>` : ''}
            <div style="display: flex; gap: 0.5rem; margin-top: 0.75rem;">
                <button type="button" class="secondary" onclick="viewMeetingDetails('${escapeHtml(meeting.id)}')" style="padding: 0.5rem 0.75rem; font-size: 0.85rem;">View Details</button>
                <button type="button" class="danger" onclick="endMeeting('${escapeHtml(meeting.id)}')" style="padding: 0.5rem 0.75rem; font-size: 0.85rem;">End Meeting</button>
            </div>
        </div>
    `).join('');
}

// View meeting details
async function viewMeetingDetails(meetingId) {
    try {
        const meeting = await apiCall(`/meetings/${encodeURIComponent(meetingId)}`);
        const modal = await formModal({
            title: `Meeting: ${meeting.title || 'Untitled'}`,
            submitText: 'Close',
            fields: [
                { id: 'id', label: 'Meeting ID', value: meeting.id, readonly: true },
                { id: 'title', label: 'Title', value: meeting.title || '', readonly: true },
                { id: 'status', label: 'Status', value: meeting.status || 'active', readonly: true },
                { id: 'started_at', label: 'Started', value: formatDate(meeting.started_at), readonly: true },
                { id: 'participants', label: 'Participants', value: (meeting.participants || []).join(', '), readonly: true },
                { id: 'description', label: 'Description', value: meeting.description || '', readonly: true, type: 'textarea' }
            ]
        });
    } catch (error) {
        showToast(`Failed to load meeting details: ${error.message}`, 'error');
    }
}

// End a meeting
async function endMeeting(meetingId) {
    if (!confirm('Are you sure you want to end this meeting?')) return;
    
    try {
        setBusy('end-meeting', true);
        await apiCall(`/meetings/${encodeURIComponent(meetingId)}/end`, {
            method: 'POST'
        });
        showToast('Meeting ended', 'success');
        await loadActiveMeetings();
        render();
    } catch (error) {
        showToast(`Failed to end meeting: ${error.message}`, 'error');
    } finally {
        setBusy('end-meeting', false);
    }
}

// Create a new meeting
async function showCreateMeetingModal() {
    const agents = state.agents || [];
    const agentOptions = agents.map(a => ({ value: a.id, label: `${a.id} (${a.persona || 'unknown'})` }));
    
    const values = await formModal({
        title: 'Create Meeting',
        submitText: 'Create',
        fields: [
            { id: 'title', label: 'Meeting Title', required: true, placeholder: 'e.g., Architecture Review' },
            { id: 'description', label: 'Description', type: 'textarea', placeholder: 'What is this meeting about?' },
            { id: 'participants', label: 'Participants (comma-separated agent IDs)', required: true, placeholder: 'agent1, agent2, agent3' }
        ]
    });
    
    if (!values) return;
    
    try {
        setBusy('create-meeting', true);
        const participants = values.participants
            .split(',')
            .map(p => p.trim())
            .filter(p => p.length > 0);
        
        if (participants.length < 2) {
            showToast('Meeting must have at least 2 participants', 'error');
            return;
        }
        
        await apiCall('/meetings', {
            method: 'POST',
            body: JSON.stringify({
                title: values.title,
                description: values.description || '',
                participants: participants
            })
        });
        
        showToast('Meeting created', 'success');
        await loadActiveMeetings();
        render();
    } catch (error) {
        showToast(`Failed to create meeting: ${error.message}`, 'error');
    } finally {
        setBusy('create-meeting', false);
    }
}
