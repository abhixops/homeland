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
document.addEventListener('htmx:afterSwap', (e) => {
    if (e.detail.target.id === 'app-grid-container') {
        e.detail.target.querySelectorAll('.animate-fade-in').forEach((el, i) => {
            el.style.animationDelay = `${i * 50}ms`;
        });
    }
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
