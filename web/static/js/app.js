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

// Clock component: current time, date, and a time-of-day greeting
function clock() {
    return {
        time: '',
        date: '',
        greeting: '',
        init() {
            this.updateTime();
            setInterval(() => this.updateTime(), 1000);
        },
        updateTime() {
            const now = new Date();
            this.time = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
            this.date = now.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
            const h = now.getHours();
            this.greeting = h < 5 ? 'Good night' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening';
        }
    };
}

// Reload-config button: asks the server to re-read homepage.yaml, then
// refreshes the page so every section (title, grid, bookmarks) shows the new
// config. If the config is invalid, the button turns red and its tooltip
// shows the error; the server keeps using the last valid config.
function reloadButton() {
    return {
        busy: false,
        error: '',
        async reload() {
            this.busy = true;
            this.error = '';
            try {
                const response = await fetch('/api/reload', { method: 'POST' });
                const data = await response.json().catch(() => ({}));
                if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
                window.location.reload();
            } catch (err) {
                this.error = err.message;
                this.busy = false;
                console.warn('[homeland] config reload failed:', err);
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
