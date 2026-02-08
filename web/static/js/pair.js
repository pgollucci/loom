// Pair-Programming Panel — slide-out chat with agents

(function () {
    'use strict';

    let pairState = {
        open: false,
        beadId: null,
        agentId: null,
        eventSource: null,
        streaming: false,
        messages: []
    };

    // Expose open/close to app.js
    window.openPairPanel = openPairPanel;
    window.closePairPanel = closePairPanel;
    window.pairState = pairState;

    function openPairPanel(beadId, agentId) {
        pairState.beadId = beadId;
        pairState.agentId = agentId || '';
        pairState.open = true;
        pairState.messages = [];

        const panel = document.getElementById('pair-panel');
        if (!panel) return;

        panel.classList.add('open');

        // Populate agent dropdown
        const select = document.getElementById('pair-agent-select');
        if (select) {
            const agents = (window.state && window.state.agents) || [];
            const available = agents.filter(a => a.status !== 'terminated');
            select.innerHTML = '<option value="">-- select agent --</option>' +
                available.map(a =>
                    `<option value="${escapeHtml(a.id)}"${a.id === agentId ? ' selected' : ''}>${escapeHtml(a.name || a.id)}</option>`
                ).join('');
        }

        // Set bead info
        const beadInfo = document.getElementById('pair-bead-info');
        if (beadInfo) {
            const bead = (window.state && window.state.beads || []).find(b => b.id === beadId);
            beadInfo.textContent = bead ? `${bead.id} — ${bead.title || '(untitled)'}` : beadId;
        }

        // Load existing conversation
        loadConversationHistory(beadId);

        // Focus the input
        const input = document.getElementById('pair-input');
        if (input) input.focus();
    }

    function closePairPanel() {
        pairState.open = false;
        pairState.beadId = null;
        pairState.agentId = null;

        if (pairState.eventSource) {
            pairState.eventSource.close();
            pairState.eventSource = null;
        }

        const panel = document.getElementById('pair-panel');
        if (panel) panel.classList.remove('open');
    }

    async function loadConversationHistory(beadId) {
        const container = document.getElementById('pair-messages');
        if (!container) return;

        container.innerHTML = '<div class="pair-loading">Loading conversation...</div>';

        try {
            const conversation = await apiCall(`/beads/${beadId}/conversation`);
            pairState.messages = (conversation && conversation.messages) || [];
            renderMessages();
        } catch (e) {
            // No existing conversation — that's fine
            pairState.messages = [];
            container.innerHTML = '<div class="pair-empty">Start a conversation with your agent.</div>';
        }
    }

    function renderMessages() {
        const container = document.getElementById('pair-messages');
        if (!container) return;

        // Filter out system messages for display
        const visible = pairState.messages.filter(m => m.role !== 'system');

        if (visible.length === 0) {
            container.innerHTML = '<div class="pair-empty">Start a conversation with your agent.</div>';
            return;
        }

        container.innerHTML = visible.map(msg => {
            const roleClass = msg.role === 'user' ? 'pair-msg-user' : 'pair-msg-assistant';
            const label = msg.role === 'user' ? 'You' : 'Agent';
            return `
                <div class="pair-msg ${roleClass}">
                    <div class="pair-msg-label">${escapeHtml(label)}</div>
                    <div class="pair-msg-content">${formatMessageContent(msg.content)}</div>
                </div>
            `;
        }).join('');

        container.scrollTop = container.scrollHeight;
    }

    function appendStreamingMessage() {
        const container = document.getElementById('pair-messages');
        if (!container) return;

        // Add a streaming placeholder
        const div = document.createElement('div');
        div.className = 'pair-msg pair-msg-assistant pair-msg-streaming';
        div.innerHTML = `
            <div class="pair-msg-label">Agent</div>
            <div class="pair-msg-content" id="pair-streaming-content"></div>
        `;
        container.appendChild(div);
        container.scrollTop = container.scrollHeight;
    }

    function updateStreamingContent(text) {
        const el = document.getElementById('pair-streaming-content');
        if (el) {
            el.innerHTML = formatMessageContent(text);
            const container = document.getElementById('pair-messages');
            if (container) container.scrollTop = container.scrollHeight;
        }
    }

    function finalizeStreamingMessage() {
        const el = document.querySelector('.pair-msg-streaming');
        if (el) el.classList.remove('pair-msg-streaming');
    }

    async function sendMessage() {
        const input = document.getElementById('pair-input');
        const agentSelect = document.getElementById('pair-agent-select');
        if (!input) return;

        const message = input.value.trim();
        if (!message) return;

        const agentId = agentSelect ? agentSelect.value : pairState.agentId;
        if (!agentId) {
            if (typeof showToast === 'function') showToast('Select an agent first.', 'error');
            return;
        }

        if (pairState.streaming) return;

        pairState.agentId = agentId;

        // Add user message to display
        pairState.messages.push({ role: 'user', content: message, timestamp: new Date().toISOString() });
        renderMessages();

        // Clear input
        input.value = '';
        input.style.height = 'auto';

        // Disable send while streaming
        pairState.streaming = true;
        updateSendButton();

        // Add streaming placeholder
        appendStreamingMessage();

        let streamedText = '';

        try {
            const response = await fetch(`${API_BASE}/pair`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    agent_id: agentId,
                    bead_id: pairState.beadId,
                    message: message
                })
            });

            if (!response.ok) {
                const err = await response.json().catch(() => ({ error: response.statusText }));
                throw new Error(err.error || 'Request failed');
            }

            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });

                // Process complete SSE events
                const lines = buffer.split('\n');
                buffer = lines.pop() || ''; // Keep incomplete line

                let eventType = '';
                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        eventType = line.slice(7).trim();
                    } else if (line.startsWith('data: ')) {
                        const data = line.slice(6);
                        handleSSEEvent(eventType, data);
                        eventType = '';
                    }
                }
            }

            // Finalize
            streamedText = document.getElementById('pair-streaming-content')?.textContent || '';
            finalizeStreamingMessage();

            // Add assistant message to our state
            pairState.messages.push({ role: 'assistant', content: streamedText, timestamp: new Date().toISOString() });

        } catch (err) {
            finalizeStreamingMessage();
            if (typeof showToast === 'function') showToast('Pair chat error: ' + err.message, 'error');
        } finally {
            pairState.streaming = false;
            updateSendButton();
        }
    }

    let accumulatedText = '';

    function handleSSEEvent(eventType, dataStr) {
        if (eventType === 'connected') {
            accumulatedText = '';
            return;
        }

        if (eventType === 'chunk') {
            try {
                const chunk = JSON.parse(dataStr);
                if (chunk.choices && chunk.choices.length > 0) {
                    const delta = chunk.choices[0].delta;
                    if (delta && delta.content) {
                        accumulatedText += delta.content;
                        updateStreamingContent(accumulatedText);
                    }
                }
            } catch (e) {
                // Ignore parse errors on chunks
            }
            return;
        }

        if (eventType === 'actions') {
            try {
                const data = JSON.parse(dataStr);
                if (data.results && data.results.length > 0) {
                    appendActionResults(data.results);
                }
            } catch (e) {
                // Ignore
            }
            return;
        }

        if (eventType === 'error') {
            try {
                const data = JSON.parse(dataStr);
                if (typeof showToast === 'function') showToast('Stream error: ' + (data.error || 'Unknown'), 'error');
            } catch (e) {
                // Ignore
            }
            return;
        }
    }

    function appendActionResults(results) {
        const container = document.getElementById('pair-messages');
        if (!container) return;

        const div = document.createElement('div');
        div.className = 'pair-msg pair-msg-actions';
        div.innerHTML = `
            <div class="pair-msg-label">Actions</div>
            <div class="pair-msg-content">
                ${results.map(r => `
                    <div class="pair-action-result ${r.status === 'executed' ? 'success' : 'error'}">
                        <strong>${escapeHtml(r.action_type)}</strong>: ${escapeHtml(r.message)}
                    </div>
                `).join('')}
            </div>
        `;
        container.appendChild(div);
        container.scrollTop = container.scrollHeight;
    }

    function updateSendButton() {
        const btn = document.getElementById('pair-send-btn');
        if (btn) {
            btn.disabled = pairState.streaming;
            btn.textContent = pairState.streaming ? 'Streaming...' : 'Send';
        }
    }

    // Initialize event listeners after DOM ready
    function initPairPanel() {
        // Send button
        const sendBtn = document.getElementById('pair-send-btn');
        if (sendBtn) sendBtn.addEventListener('click', sendMessage);

        // Textarea: Enter to send, Shift+Enter for newlines
        const input = document.getElementById('pair-input');
        if (input) {
            input.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    sendMessage();
                }
            });
            // Auto-resize
            input.addEventListener('input', () => {
                input.style.height = 'auto';
                input.style.height = Math.min(input.scrollHeight, 150) + 'px';
            });
        }

        // Close button
        const closeBtn = document.getElementById('pair-close-btn');
        if (closeBtn) closeBtn.addEventListener('click', closePairPanel);

        // Agent selector change
        const agentSelect = document.getElementById('pair-agent-select');
        if (agentSelect) {
            agentSelect.addEventListener('change', (e) => {
                pairState.agentId = e.target.value;
            });
        }
    }

    // Run init when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initPairPanel);
    } else {
        initPairPanel();
    }
})();
