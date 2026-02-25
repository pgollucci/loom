/**
 * Loom Debug System — web/static/js/debug.js
 *
 * Three levels:
 *   "off"      — no output
 *   "standard" — CRUD at the edges: bead/agent/project lifecycle, all HTTP errors
 *   "extreme"  — everything: clicks, every fetch body, SSE messages, forms, navigation
 *
 * All output: console.debug('[LOOM_DEBUG]', JSON.stringify(entry))
 * Schema: see docs/DEBUG.md
 *
 * Configure via:
 *   localStorage.setItem('LOOM_DEBUG_LEVEL', 'extreme')  — browser-side override
 *   GET /api/v1/config/debug                              — server default
 *   LoomDebug.setLevel('extreme')                         — programmatic
 */
(function (window) {
    'use strict';

    var SCHEMA_VERSION = '1';
    var LEVELS = { 'off': 0, 'standard': 1, 'extreme': 2 };

    var _level = 'off';
    var _seq = 0;
    var _emitting = false;
    var _fetchPatched = false;
    var _esPatched = false;
    var _clicksPatched = false;
    var _consolePatched = false;

    // Save original fetch BEFORE any patching
    var _origFetch = window.fetch;

    // ─── Core emit ────────────────────────────────────────────────────────────

    function _emit(category, action, data, source, minLevel) {
        if (_emitting) return;                         // prevent recursion
        if (!minLevel) minLevel = 'extreme';
        if (LEVELS[_level] < LEVELS[minLevel]) return; // below threshold

        _emitting = true;
        try {
            var entry = {
                ts: new Date().toISOString(),
                seq: ++_seq,
                schema: SCHEMA_VERSION,
                debug_level: _level,
                category: category,
                action: action,
                source: source || 'unknown',
                data: data || {}
            };
            // Hoist duration_ms from data to top level if present
            if (data && data._duration_ms !== undefined) {
                entry.duration_ms = data._duration_ms;
                delete entry.data._duration_ms;
            }
            console.debug('[LOOM_DEBUG]', JSON.stringify(entry));
        } finally {
            _emitting = false;
        }
    }

    // ─── Standard URL patterns ────────────────────────────────────────────────
    // Requests matching these are emitted at "standard" level with semantic categories.

    var STANDARD_PATTERNS = [
        // Bead lifecycle
        { method: 'POST',   re: /\/api\/v1\/beads$/,                    category: 'bead_event',    action: 'bead created' },
        { method: 'PUT',    re: /\/api\/v1\/beads\/[^/]+$/,             category: 'bead_event',    action: 'bead updated' },
        { method: 'PATCH',  re: /\/api\/v1\/beads\/[^/]+$/,             category: 'bead_event',    action: 'bead updated' },
        { method: 'DELETE', re: /\/api\/v1\/beads\/[^/]+$/,             category: 'bead_event',    action: 'bead deleted' },
        { method: 'POST',   re: /\/api\/v1\/beads\/[^/]+\/close$/,      category: 'bead_event',    action: 'bead closed' },
        { method: 'POST',   re: /\/api\/v1\/beads\/[^/]+\/claim$/,      category: 'bead_event',    action: 'bead claimed' },
        { method: 'POST',   re: /\/api\/v1\/beads\/[^/]+\/block$/,      category: 'bead_event',    action: 'bead blocked' },
        { method: 'POST',   re: /\/api\/v1\/beads\/[^/]+\/redispatch$/, category: 'bead_event',    action: 'bead redispatched' },
        { method: 'POST',   re: /\/api\/v1\/beads\/[^/]+\/annotate$/,   category: 'bead_event',    action: 'bead annotated' },
        // Agent lifecycle
        { method: 'POST',   re: /\/api\/v1\/agents$/,                   category: 'agent_event',   action: 'agent created' },
        { method: 'DELETE', re: /\/api\/v1\/agents\/[^/]+$/,            category: 'agent_event',   action: 'agent deleted' },
        { method: 'POST',   re: /\/api\/v1\/agents\/[^/]+\/start$/,     category: 'agent_event',   action: 'agent started' },
        { method: 'POST',   re: /\/api\/v1\/agents\/[^/]+\/stop$/,      category: 'agent_event',   action: 'agent stopped' },
        { method: 'POST',   re: /\/api\/v1\/agents\/[^/]+\/pause$/,     category: 'agent_event',   action: 'agent paused' },
        { method: 'POST',   re: /\/api\/v1\/agents\/[^/]+\/resume$/,    category: 'agent_event',   action: 'agent resumed' },
        // Project lifecycle
        { method: 'POST',   re: /\/api\/v1\/projects$/,                 category: 'project_event', action: 'project created' },
        { method: 'POST',   re: /\/api\/v1\/projects\/bootstrap$/,      category: 'project_event', action: 'project bootstrapped' },
        { method: 'DELETE', re: /\/api\/v1\/projects\/[^/]+$/,          category: 'project_event', action: 'project deleted' },
        { method: 'PUT',    re: /\/api\/v1\/projects\/[^/]+$/,          category: 'project_event', action: 'project updated' },
        { method: 'POST',   re: /\/api\/v1\/projects\/[^/]+\/close$/,   category: 'project_event', action: 'project closed' },
    ];

    function _matchStandard(method, url) {
        for (var i = 0; i < STANDARD_PATTERNS.length; i++) {
            var p = STANDARD_PATTERNS[i];
            if (p.method === method && p.re.test(url)) return p;
        }
        return null;
    }

    // ─── Fetch instrumentation ────────────────────────────────────────────────

    function _patchFetch() {
        if (_fetchPatched) return;
        _fetchPatched = true;

        window.fetch = function (input, init) {
            var method = (init && init.method) || 'GET';
            var url = typeof input === 'string' ? input
                     : (input && input.url) ? input.url
                     : String(input);
            var startMs = performance.now();

            // Emit request event (extreme only)
            if (LEVELS[_level] >= LEVELS['extreme']) {
                var reqBody;
                if (init && init.body) {
                    try { reqBody = JSON.parse(init.body); }
                    catch (e) { reqBody = String(init.body).slice(0, 512); }
                }
                var hdrs = {};
                if (init && init.headers) {
                    try {
                        if (init.headers instanceof Headers) {
                            init.headers.forEach(function (v, k) { hdrs[k] = v; });
                        } else {
                            hdrs = init.headers;
                        }
                    } catch (e) {}
                }
                _emit('api_request', method + ' ' + url, {
                    method: method,
                    url: url,
                    headers: hdrs,
                    body: reqBody,
                }, 'fetch', 'extreme');
            }

            return _origFetch.apply(window, arguments).then(function (response) {
                var durationMs = Math.round(performance.now() - startMs);
                var status = response.status;
                var ok = response.ok;
                var stdMatch = _matchStandard(method, url);
                var isError = !ok;
                var shouldLog = LEVELS[_level] >= LEVELS['extreme'] ||
                    (LEVELS[_level] >= LEVELS['standard'] && (isError || stdMatch));

                if (shouldLog) {
                    response.clone().text().then(function (text) {
                        var body = null;
                        try { body = JSON.parse(text); }
                        catch (e) { body = text.slice(0, 512) || null; }

                        var category = isError ? 'api_error'
                                      : stdMatch ? stdMatch.category
                                      : 'api_response';
                        var action = stdMatch
                            ? stdMatch.action + ' (' + status + ')'
                            : method + ' ' + url + ' \u2192 ' + status;
                        var data = {
                            method: method,
                            url: url,
                            status: status,
                            ok: ok,
                            _duration_ms: durationMs,
                        };
                        if (LEVELS[_level] >= LEVELS['extreme']) {
                            data.response_body = body;
                        } else if (isError) {
                            data.error = body;
                        }
                        var minLevel = (stdMatch || isError) ? 'standard' : 'extreme';
                        _emit(category, action, data, 'fetch', minLevel);
                    }).catch(function () {
                        var category = isError ? 'api_error' : (stdMatch ? stdMatch.category : 'api_response');
                        var action = stdMatch ? stdMatch.action + ' (' + status + ')' : method + ' ' + url + ' \u2192 ' + status;
                        var minLevel = (stdMatch || isError) ? 'standard' : 'extreme';
                        _emit(category, action, { method: method, url: url, status: status, ok: ok, _duration_ms: durationMs }, 'fetch', minLevel);
                    });
                }
                return response;

            }).catch(function (err) {
                var durationMs = Math.round(performance.now() - startMs);
                _emit('api_error', method + ' ' + url + ' \u2192 NETWORK ERROR', {
                    method: method,
                    url: url,
                    error: err.message,
                    _duration_ms: durationMs,
                }, 'fetch', 'standard');
                throw err;
            });
        };
    }

    // ─── Click instrumentation ────────────────────────────────────────────────

    function _patchClicks() {
        if (_clicksPatched) return;
        _clicksPatched = true;

        document.addEventListener('click', function (e) {
            if (LEVELS[_level] < LEVELS['extreme']) return;

            var target = e.target;
            var el = (target.closest
                ? target.closest('button, a, [role="button"], input[type="submit"], input[type="button"], select, input[type="checkbox"], input[type="radio"]')
                : null) || target;

            var dataAttrs = null;
            var hasData = false;
            var attrs = el.attributes;
            for (var i = 0; i < attrs.length; i++) {
                if (attrs[i].name.indexOf('data-') === 0) {
                    if (!dataAttrs) dataAttrs = {};
                    dataAttrs[attrs[i].name] = attrs[i].value;
                    hasData = true;
                }
            }

            _emit('ui_click', 'click: ' + el.tagName.toLowerCase() + (el.id ? '#' + el.id : ''), {
                tag: el.tagName.toLowerCase(),
                id: el.id || null,
                classes: el.className ? String(el.className).trim().slice(0, 120) : null,
                text: ((el.textContent || el.value || '')).trim().slice(0, 120),
                href: el.href || null,
                type: el.type || null,
                name: el.name || null,
                value: (el.type === 'checkbox' || el.type === 'radio') ? el.checked : (el.value || null),
                'data-attrs': hasData ? dataAttrs : null,
                x: Math.round(e.clientX),
                y: Math.round(e.clientY),
            }, 'dom', 'extreme');
        }, true /* capture phase — catches all clicks */);
    }

    // ─── Form instrumentation ─────────────────────────────────────────────────

    function _patchForms() {
        document.addEventListener('submit', function (e) {
            if (LEVELS[_level] < LEVELS['extreme']) return;

            var form = e.target;
            var fields = {};
            var els = form.elements;
            for (var i = 0; i < els.length; i++) {
                var el = els[i];
                if (!el.name) continue;
                if (el.type === 'password') { fields[el.name] = '[REDACTED]'; continue; }
                if (el.type === 'checkbox' || el.type === 'radio') { fields[el.name] = el.checked; continue; }
                fields[el.name] = el.value;
            }

            _emit('ui_form', 'form submit: ' + (form.id || form.action || 'unknown'), {
                form_id: form.id || null,
                action: form.action || null,
                method: (form.method || 'get').toUpperCase(),
                fields: fields,
            }, 'dom', 'extreme');
        }, true);
    }

    // ─── Navigation instrumentation ───────────────────────────────────────────

    function _patchNavigation() {
        window.addEventListener('hashchange', function (e) {
            if (LEVELS[_level] < LEVELS['extreme']) return;
            _emit('navigation', 'hashchange', { from: e.oldURL, to: e.newURL }, 'navigation', 'extreme');
        });

        document.addEventListener('visibilitychange', function () {
            if (LEVELS[_level] < LEVELS['extreme']) return;
            _emit('navigation', 'visibility_change', {
                hidden: document.hidden,
                state: document.visibilityState,
            }, 'navigation', 'extreme');
        });

        // Semantic tab navigation (view-tabs use data-target attribute)
        document.addEventListener('click', function (e) {
            if (LEVELS[_level] < LEVELS['extreme']) return;
            var tab = e.target.closest ? e.target.closest('[data-target]') : null;
            if (!tab) return;
            _emit('navigation', 'tab switch: ' + tab.dataset.target, {
                target: tab.dataset.target,
                label: (tab.textContent || '').trim().slice(0, 80),
            }, 'navigation', 'extreme');
        });
    }

    // ─── EventSource / SSE instrumentation ───────────────────────────────────

    function _patchEventSource() {
        if (_esPatched || !window.EventSource) return;
        _esPatched = true;

        var _OrigES = window.EventSource;

        function PatchedEventSource(url, init) {
            var es = new _OrigES(url, init);

            _emit('sse_connect', 'SSE open: ' + url, { url: url }, 'eventsource', 'extreme');

            es.addEventListener('error', function () {
                _emit('sse_error', 'SSE error: ' + url, { url: url, readyState: es.readyState }, 'eventsource', 'standard');
            });

            var _origAdd = es.addEventListener.bind(es);
            es.addEventListener = function (type, handler, opts) {
                var wrappedHandler = function (event) {
                    if (LEVELS[_level] >= LEVELS['extreme']) {
                        var evData = event.data;
                        try { evData = JSON.parse(event.data); } catch (e) {}
                        _emit('sse_event', 'SSE message: ' + type, {
                            event_type: type,
                            last_event_id: event.lastEventId || null,
                            data: evData,
                        }, 'eventsource', 'extreme');
                    }
                    return handler.call(this, event);
                };
                return _origAdd(type, wrappedHandler, opts);
            };

            return es;
        }

        PatchedEventSource.CONNECTING = _OrigES.CONNECTING;
        PatchedEventSource.OPEN = _OrigES.OPEN;
        PatchedEventSource.CLOSED = _OrigES.CLOSED;
        PatchedEventSource.prototype = _OrigES.prototype;
        window.EventSource = PatchedEventSource;
    }

    // ─── Console interception ─────────────────────────────────────────────────

    function _patchConsole() {
        if (_consolePatched) return;
        _consolePatched = true;

        var _origError = console.error;
        console.error = function () {
            if (LEVELS[_level] >= LEVELS['extreme']) {
                var msg = Array.prototype.slice.call(arguments).map(function (a) {
                    return (typeof a === 'object') ? JSON.stringify(a) : String(a);
                }).join(' ').slice(0, 1000);
                _emit('error', 'console.error', { message: msg }, 'console', 'extreme');
            }
            return _origError.apply(console, arguments);
        };

        var _origWarn = console.warn;
        console.warn = function () {
            if (LEVELS[_level] >= LEVELS['extreme']) {
                var msg = Array.prototype.slice.call(arguments).map(function (a) {
                    return (typeof a === 'object') ? JSON.stringify(a) : String(a);
                }).join(' ').slice(0, 500);
                _emit('error', 'console.warn', { message: msg }, 'console', 'extreme');
            }
            return _origWarn.apply(console, arguments);
        };
    }

    // ─── Init ─────────────────────────────────────────────────────────────────

    function _install() {
        if (LEVELS[_level] === 0) return;

        _patchFetch();

        if (LEVELS[_level] >= LEVELS['extreme']) {
            _patchClicks();
            _patchForms();
            _patchNavigation();
            _patchEventSource();
            _patchConsole();
        }

        _emit('system', 'debug system initialized', {
            level: _level,
            schema_version: SCHEMA_VERSION,
            url: window.location.href,
            user_agent: navigator.userAgent.slice(0, 120),
        }, 'debug.js', 'standard');

        console.info(
            '%c[LOOM_DEBUG] Level: ' + _level +
            ' \u2014 JSON events emitted via console.debug(). Filter: [LOOM_DEBUG]',
            'color:#00aaff;font-weight:bold'
        );
    }

    function _init() {
        // Priority 1: localStorage override (developer preference)
        var localOverride = null;
        try { localOverride = localStorage.getItem('LOOM_DEBUG_LEVEL'); } catch (e) {}

        if (localOverride && LEVELS[localOverride] !== undefined) {
            _level = localOverride;
            _install();
            return;
        }

        // Priority 2: server config
        _origFetch('/api/v1/config/debug').then(function (resp) {
            if (resp.ok) {
                return resp.json().then(function (cfg) {
                    if (cfg.level && LEVELS[cfg.level] !== undefined) {
                        _level = cfg.level;
                    }
                    _install();
                });
            }
            _install(); // server returned error — stay at 'off'
        }).catch(function () {
            _install(); // network error — stay at 'off'
        });
    }

    // ─── Public API ───────────────────────────────────────────────────────────

    window.LoomDebug = {
        /**
         * Emit a debug event.
         * @param {string} category  - See docs/DEBUG.md for categories
         * @param {string} action    - Human-readable description
         * @param {object} [data]    - Arbitrary event data
         * @param {string} [source]  - Emitting component name
         * @param {string} [minLevel] - 'standard' | 'extreme' (default: 'extreme')
         */
        dbg: function (category, action, data, source, minLevel) {
            _emit(category, action, data, source || 'app', minLevel || 'extreme');
        },

        /** Emit at standard level (bead/agent/project events, errors) */
        standard: function (category, action, data, source) {
            _emit(category, action, data, source || 'app', 'standard');
        },

        /** Emit at extreme level */
        extreme: function (category, action, data, source) {
            _emit(category, action, data, source || 'app', 'extreme');
        },

        /** Current active debug level */
        level: function () { return _level; },

        /** Set level programmatically; persists to localStorage */
        setLevel: function (l) {
            if (LEVELS[l] === undefined) {
                throw new Error('Invalid debug level: "' + l + '". Valid: off, standard, extreme');
            }
            var prev = _level;
            _level = l;
            try { localStorage.setItem('LOOM_DEBUG_LEVEL', l); } catch (e) {}
            if (LEVELS[_level] > 0) _install();
            _emit('system', 'debug level changed', { from: prev, to: l }, 'debug.js', 'standard');
        },

        /** Remove localStorage override; reload to use server default */
        clearOverride: function () {
            try { localStorage.removeItem('LOOM_DEBUG_LEVEL'); } catch (e) {}
            console.info('[LOOM_DEBUG] Override cleared. Reload page to use server-configured level.');
        },
    };

    // Start
    _init();

}(window));
