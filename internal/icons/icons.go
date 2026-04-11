// Package icons handles fetching and caching of app icons from the selfh.st icon repository.
package icons

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The selfh.st icons CDN base URL (via jsDelivr).
const cdnBaseURL = "https://cdn.jsdelivr.net/gh/selfhst/icons/svg/"

// Default SVG icon used when the requested icon cannot be found.
const defaultIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>`

// Retry configuration for CDN fetches.
const (
	maxRetries    = 3
	retryBaseWait = 2 * time.Second
)

// Fetcher manages icon downloads and local caching.
type Fetcher struct {
	cacheDir   string
	mu         sync.Mutex
	fetching   map[string]bool   // tracks in-progress fetches to avoid duplicates
	failed     map[string]int    // tracks failed fetches and attempt count for retry
	failedMu   sync.Mutex
	httpClient *http.Client
}

// NewFetcher creates a new icon fetcher that caches files in the given directory.
func NewFetcher(cacheDir string) *Fetcher {
	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("[icons] failed to create cache dir %s: %v", cacheDir, err)
	}
	f := &Fetcher{
		cacheDir: cacheDir,
		fetching: make(map[string]bool),
		failed:   make(map[string]int),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	// Write default fallback icon immediately so it's always available
	f.ensureDefault()
	return f
}

// GetIconPath returns the local path for an icon. If the icon is already cached,
// it returns the cached path immediately. Otherwise, it triggers a background fetch
// and returns the default icon path.
func (f *Fetcher) GetIconPath(iconName string) string {
	if iconName == "" {
		return f.ensureDefault()
	}

	// Normalize: strip extensions, lowercase
	cleanName := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(iconName), ".svg"), ".png")
	fileName := cleanName + ".svg"
	cachedPath := filepath.Join(f.cacheDir, fileName)

	// Check if already cached
	if _, err := os.Stat(cachedPath); err == nil {
		return "/static/icons/" + fileName
	}

	// Trigger async fetch
	go f.fetch(cleanName, cachedPath)
	return "/static/icons/default.svg"
}

// FetchIconSync fetches an icon synchronously. Returns the URL path for the icon.
// Used during startup preloading to ensure icons are ready before serving requests.
func (f *Fetcher) FetchIconSync(iconName string) string {
	if iconName == "" {
		return f.ensureDefault()
	}

	cleanName := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(iconName), ".svg"), ".png")
	fileName := cleanName + ".svg"
	cachedPath := filepath.Join(f.cacheDir, fileName)

	// Check if already cached
	if _, err := os.Stat(cachedPath); err == nil {
		return "/static/icons/" + fileName
	}

	// Fetch synchronously (blocking)
	f.fetch(cleanName, cachedPath)

	// Check if fetch succeeded
	if _, err := os.Stat(cachedPath); err == nil {
		return "/static/icons/" + fileName
	}
	return "/static/icons/default.svg"
}

// GetIconURL returns the URL path suitable for HTML src attributes.
func (f *Fetcher) GetIconURL(iconName string) string {
	return f.GetIconPath(iconName)
}

// fetch downloads an icon from the CDN with retry logic and saves it to the cache directory.
func (f *Fetcher) fetch(name, destPath string) {
	f.mu.Lock()
	if f.fetching[name] {
		f.mu.Unlock()
		return
	}
	f.fetching[name] = true
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.fetching, name)
		f.mu.Unlock()
	}()

	url := cdnBaseURL + name + ".svg"

	// Retry with exponential backoff
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := f.download(url, destPath); err != nil {
			lastErr = err
			if attempt < maxRetries {
				wait := retryBaseWait * time.Duration(1<<(attempt-1))
				log.Printf("[icons] fetch %s attempt %d/%d failed: %v — retrying in %v", name, attempt, maxRetries, err, wait)
				time.Sleep(wait)
				continue
			}
		} else {
			log.Printf("[icons] cached icon: %s", name)
			// Clear from failed list on success
			f.failedMu.Lock()
			delete(f.failed, name)
			f.failedMu.Unlock()
			return
		}
	}

	// All retries exhausted — track for periodic re-fetch
	log.Printf("[icons] failed to fetch %s after %d attempts: %v", name, maxRetries, lastErr)
	f.failedMu.Lock()
	f.failed[name]++
	f.failedMu.Unlock()
}

// download fetches a URL and writes the response body to a file.
func (f *Fetcher) download(url, destPath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "Homeland-Dashboard/1.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(destPath) // clean up partial file
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

// ensureDefault writes the default icon to cache if it doesn't exist and returns its path.
func (f *Fetcher) ensureDefault() string {
	defaultPath := filepath.Join(f.cacheDir, "default.svg")
	if _, err := os.Stat(defaultPath); err != nil {
		os.WriteFile(defaultPath, []byte(defaultIconSVG), 0644)
	}
	return "/static/icons/default.svg"
}

// RetryFailed attempts to re-fetch any icons that previously failed.
// Call this periodically (e.g. every 5 minutes) to recover from transient network issues.
func (f *Fetcher) RetryFailed() {
	f.failedMu.Lock()
	toRetry := make(map[string]int, len(f.failed))
	for name, count := range f.failed {
		// Give up after 10 total retry cycles to avoid infinite loops for truly missing icons
		if count <= 10 {
			toRetry[name] = count
		}
	}
	f.failedMu.Unlock()

	if len(toRetry) == 0 {
		return
	}

	log.Printf("[icons] retrying %d previously failed icons", len(toRetry))
	for name := range toRetry {
		cleanName := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(name), ".svg"), ".png")
		fileName := cleanName + ".svg"
		cachedPath := filepath.Join(f.cacheDir, fileName)

		// Only retry if still not cached
		if _, err := os.Stat(cachedPath); err != nil {
			f.fetch(cleanName, cachedPath)
		} else {
			f.failedMu.Lock()
			delete(f.failed, name)
			f.failedMu.Unlock()
		}
	}
}

// StartRetryLoop starts a background goroutine that periodically retries failed icon fetches.
func (f *Fetcher) StartRetryLoop(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				f.RetryFailed()
			}
		}
	}()
}

// PreloadIcons fetches all icons synchronously in parallel with a global timeout.
// This ensures icons are cached before the server starts accepting requests.
func (f *Fetcher) PreloadIcons(iconNames []string) {
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		// Limit concurrency to avoid overwhelming the CDN
		sem := make(chan struct{}, 5)
		for _, name := range iconNames {
			if name == "" {
				continue
			}
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				f.FetchIconSync(n)
			}(name)
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[icons] preloaded %d icons", len(iconNames))
	case <-time.After(30 * time.Second):
		log.Printf("[icons] preload timed out after 30s, continuing with cached icons")
	}
}
