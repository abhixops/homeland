// Package metrics collects system metrics (CPU, memory, uptime) for the dashboard widgets.
package metrics

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemMetrics holds the latest system resource usage snapshot.
type SystemMetrics struct {
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryPercent   float64   `json:"memory_percent"`
	MemoryUsedGB    float64   `json:"memory_used_gb"`
	MemoryTotalGB   float64   `json:"memory_total_gb"`
	UptimeSeconds   uint64    `json:"uptime_seconds"`
	UptimeFormatted string    `json:"uptime_formatted"`
	NumCPUs         int       `json:"num_cpus"`
	ContainerCount  int       `json:"container_count"`
	CollectedAt     time.Time `json:"collected_at"`
}

// ContainerCounter is an interface to get the running container count,
// allowing decoupling from the Docker package.
type ContainerCounter interface {
	ContainerCount() int
}

// Collector periodically gathers system metrics.
type Collector struct {
	mu               sync.RWMutex
	metrics          SystemMetrics
	interval         time.Duration
	containerCounter ContainerCounter
	stop             chan struct{}
}

// NewCollector creates a metrics collector with the given refresh interval.
func NewCollector(intervalSec int, cc ContainerCounter) *Collector {
	return &Collector{
		interval:         time.Duration(intervalSec) * time.Second,
		containerCounter: cc,
		stop:             make(chan struct{}),
	}
}

// Start begins periodic metrics collection in a background goroutine.
func (c *Collector) Start() {
	// Collect immediately
	c.collect()

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-ticker.C:
				c.collect()
			}
		}
	}()
	log.Println("[metrics] started metrics collection")
}

// Stop cancels the metrics collection goroutine.
func (c *Collector) Stop() {
	close(c.stop)
}

// Get returns the latest metrics snapshot.
func (c *Collector) Get() SystemMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}

// collect gathers all system metrics.
func (c *Collector) collect() {
	m := SystemMetrics{
		CollectedAt: time.Now(),
		NumCPUs:     runtime.NumCPU(),
	}

	// CPU usage (average over 1 second)
	if cpuPercent, err := cpu.Percent(time.Second, false); err == nil && len(cpuPercent) > 0 {
		m.CPUPercent = cpuPercent[0]
	}

	// Memory usage
	if vmStat, err := mem.VirtualMemory(); err == nil {
		m.MemoryPercent = vmStat.UsedPercent
		m.MemoryUsedGB = float64(vmStat.Used) / (1024 * 1024 * 1024)
		m.MemoryTotalGB = float64(vmStat.Total) / (1024 * 1024 * 1024)
	}

	// Host uptime
	if uptime, err := host.Uptime(); err == nil {
		m.UptimeSeconds = uptime
		m.UptimeFormatted = formatUptime(uptime)
	}

	// Container count
	if c.containerCounter != nil {
		m.ContainerCount = c.containerCounter.ContainerCount()
	}

	c.mu.Lock()
	c.metrics = m
	c.mu.Unlock()
}

// formatUptime converts seconds to a human-readable format like "3d 14h 22m".
func formatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
