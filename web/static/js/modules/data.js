// Data loading module - handles all API data fetching
import { apiCall } from './api.js';
import { state } from './shared.js';

export async function loadBeads() {
    state.beads = await apiCall('/beads');
}

export async function loadCeoBeads() {
    const ceoIds = getCeoAgentIds();
    if (ceoIds.length === 0) {
        state.ceoBeads = [];
        return;
    }
    const query = encodeURIComponent(ceoIds.join(','));
    state.ceoBeads = await apiCall(`/beads?assigned_to=${query}`);
}

export async function loadAgents() {
    state.agents = await apiCall('/agents');
}

export async function loadProjects() {
    state.projects = await apiCall('/projects');
}

export async function loadPersonas() {
    state.personas = await apiCall('/personas');
}

export async function loadDecisions() {
    const inMemory = await apiCall('/decisions').catch(() => []);
    const beadDecisions = await apiCall('/beads?type=decision').catch(() => []);
    const seen = new Set((inMemory || []).map(d => d.id));
    const merged = [...(inMemory || [])];
    for (const b of (beadDecisions || [])) {
        if (!seen.has(b.id)) {
            merged.push({
                ...b,
                question: b.title || b.description || '',
                requester_id: (b.context && b.context.requester_id) || b.assigned_to || 'system',
                recommendation: (b.context && b.context.recommendation) || ''
            });
            seen.add(b.id);
        }
    }
    state.decisions = merged;
}

export async function loadSystemStatus() {
    state.systemStatus = await apiCall('/system/status');
}

export async function loadUsers() {
    try {
        state.users = await apiCall('/auth/users', { suppressToast: true, skipAutoFile: true });
    } catch (error) {
        state.users = [];
    }
}

export async function loadAPIKeys() {
    try {
        state.apiKeys = await apiCall('/auth/api-keys', { suppressToast: true, skipAutoFile: true });
    } catch (error) {
        state.apiKeys = [];
    }
}

export async function loadActiveMeetings() {
    try {
        state.activeMeetings = await apiCall('/meetings/active', { suppressToast: true });
    } catch (error) {
        state.activeMeetings = [];
    }
}

export async function loadStatusBoardFeed() {
    try {
        state.statusBoardFeed = await apiCall('/status-board/feed', { suppressToast: true });
    } catch (error) {
        state.statusBoardFeed = [];
    }
}

export async function loadOrgHealth() {
    try {
        state.orgHealth = await apiCall('/org/health', { suppressToast: true });
    } catch (error) {
        state.orgHealth = {};
    }
}

export async function loadReviewSummary() {
    try {
        state.reviewSummary = await apiCall('/reviews/summary', { suppressToast: true });
    } catch (error) {
        state.reviewSummary = {};
    }
}

export async function loadEscalationQueue() {
    try {
        state.escalationQueue = await apiCall('/escalations/queue', { suppressToast: true });
    } catch (error) {
        state.escalationQueue = [];
    }
}

function getCeoAgentIds() {
    return state.agents
        .filter(a => a.persona && a.persona.name && a.persona.name.includes('CEO'))
        .map(a => a.id);
}
