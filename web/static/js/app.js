// ── Alpine.js Stores & Utilities ──

// Dark mode store with localStorage persistence
document.addEventListener('alpine:init', () => {
    // Read server-side theme preference from meta tag
    const serverTheme = document.querySelector('meta[name="theme"]')?.content;

    Alpine.store('darkMode', {
        on: localStorage.getItem('darkMode') !== null
            ? localStorage.getItem('darkMode') === 'true'
            : (serverTheme === 'dark' || window.matchMedia('(prefers-color-scheme: dark)').matches),

        toggle() {
            this.on = !this.on;
            localStorage.setItem('darkMode', this.on);
            this.apply();
        },

        apply() {
            if (this.on) {
                document.documentElement.classList.add('dark');
            } else {
                document.documentElement.classList.remove('dark');
            }
        },

        init() {
            // Apply dark mode immediately on init
            this.apply();
        }
    });
});

// Clock component showing current time
function clock() {
    return {
        time: '',
        init() {
            this.updateTime();
            setInterval(() => this.updateTime(), 1000);
        },
        updateTime() {
            const now = new Date();
            this.time = now.toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit',
                hour12: true
            });
        }
    };
}

// Docker Compose sidebar control. The server restricts operations to the
// configured sources directory; this component only presents those projects.
function composeControls() {
    return {
        projects: [],
        project: '',
        loading: true,
        running: false,
        failed: false,
        message: '',
        async loadProjects() {
            this.loading = true;
            this.message = '';
            try {
                const response = await fetch('/api/compose/projects');
                const data = await response.json();
                if (!response.ok) throw new Error(data.error || 'Unable to load projects');
                this.projects = data.projects || [];
            } catch (error) {
                this.failed = true;
                this.message = error.message;
            } finally {
                this.loading = false;
            }
        },
        async run(action) {
            this.running = true;
            this.failed = false;
            this.message = `${action === 'up' ? 'Starting' : 'Stopping'} ${this.project}…`;
            try {
                const response = await fetch('/api/compose', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ project: this.project, action })
                });
                const data = await response.json();
                if (!response.ok) throw new Error(data.error || 'Compose command failed');
                this.message = `${this.project} ${action === 'up' ? 'started' : 'stopped'}.`;
            } catch (error) {
                this.failed = true;
                this.message = error.message;
            } finally {
                this.running = false;
            }
        }
    };
}

// ── Keyboard Shortcuts ──
document.addEventListener('keydown', (e) => {
    // "/" to focus search bar
    if (e.key === '/' && !isInputFocused()) {
        e.preventDefault();
        const searchInput = document.getElementById('search-input');
        if (searchInput) {
            searchInput.focus();
            searchInput.select();
        }
    }

    // "Escape" to blur search and clear results
    if (e.key === 'Escape') {
        const searchInput = document.getElementById('search-input');
        if (searchInput && document.activeElement === searchInput) {
            searchInput.blur();
            searchInput.value = '';
            const results = document.getElementById('search-results');
            if (results) results.innerHTML = '';
        }
    }
});

function isInputFocused() {
    const tag = document.activeElement?.tagName?.toLowerCase();
    return tag === 'input' || tag === 'textarea' || tag === 'select';
}

// ── HTMX Event Hooks ──

// Suppress swaps on error responses — prevents error HTML from replacing working content
document.addEventListener('htmx:beforeSwap', (e) => {
    const status = e.detail.xhr.status;
    // If the server returned an error, keep existing content instead of swapping in error HTML
    if (status >= 400 || status === 0) {
        e.detail.shouldSwap = false;
        e.detail.isError = false; // prevent HTMX from showing error state
        console.warn(`[homeland] HTMX request to ${e.detail.pathInfo?.requestPath || 'unknown'} failed (${status}), keeping current content`);
    }
});

// Log response errors silently without disrupting the UI
document.addEventListener('htmx:responseError', (e) => {
    console.warn('[homeland] HTMX response error:', e.detail);
});

// Handle connection errors (server unreachable) — prevent swap
document.addEventListener('htmx:sendError', (e) => {
    console.warn('[homeland] HTMX send error — server may be unreachable');
});

// Close search results when clicking outside
document.addEventListener('click', (e) => {
    const searchResults = document.getElementById('search-results');
    const searchInput = document.getElementById('search-input');
    if (searchResults && searchInput &&
        !searchResults.contains(e.target) &&
        !searchInput.contains(e.target)) {
        searchResults.innerHTML = '';
    }
});
