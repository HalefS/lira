package data

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPUReading is a point-in-time CPU usage reading. On Linux it's the host's
// total CPU usage (all cores, all processes) read from /proc/stat. Other
// platforms don't have a stdlib-only way to read that, so Available is false
// there and the frontend shows "not available on this OS" instead of a
// number.
type CPUReading struct {
	Available bool    `json:"available"`
	Percent   float64 `json:"percent"`
	Cores     int     `json:"cores"`
}

// MemReading is a point-in-time memory reading. On Linux it's host-wide,
// read from /proc/meminfo. Elsewhere it falls back to this process's own
// memory (Go's runtime.MemStats), which is a real number but a different
// (smaller) thing than "how full is the machine" — SystemWide says which one
// the caller got.
type MemReading struct {
	SystemWide bool    `json:"system_wide"`
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"` // 0 when unknown (process fallback)
	Percent    float64 `json:"percent"`     // 0 when TotalBytes is unknown
}

// cpuSample is one /proc/stat reading: cumulative jiffies since boot.
type cpuSample struct {
	idle, total uint64
}

// SystemSampler takes CPU readings. CPU usage only means something as a
// delta between two points in time, so it keeps the previous /proc/stat
// reading between calls — the first call after the process starts has
// nothing to compare against, so it reports unavailable just for that call.
type SystemSampler struct {
	mu   sync.Mutex
	prev *cpuSample
}

func NewSystemSampler() *SystemSampler {
	return &SystemSampler{}
}

func (s *SystemSampler) ReadCPU() CPUReading {
	cur, ok := readProcStat()
	if !ok {
		return CPUReading{Available: false, Cores: runtime.NumCPU()}
	}

	s.mu.Lock()
	prev := s.prev
	s.prev = &cur
	s.mu.Unlock()

	if prev == nil {
		return CPUReading{Available: false, Cores: runtime.NumCPU()}
	}

	totalDelta := float64(cur.total - prev.total)
	idleDelta := float64(cur.idle - prev.idle)
	if totalDelta <= 0 {
		return CPUReading{Available: false, Cores: runtime.NumCPU()}
	}

	percent := (1 - idleDelta/totalDelta) * 100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return CPUReading{Available: true, Percent: round1(percent), Cores: runtime.NumCPU()}
}

// readProcStat reads the aggregate "cpu" line of /proc/stat (all cores
// combined). Returns ok=false on any platform where that file doesn't
// exist or doesn't parse — i.e. everywhere but Linux.
func readProcStat() (cpuSample, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return cpuSample{}, false
	}
	fields := strings.Fields(scanner.Text())
	// fields[0] == "cpu", fields[1:] == user, nice, system, idle, iowait, irq, softirq, steal, ...
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, false
	}

	var total uint64
	var idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return cpuSample{}, false
		}
		total += v
		if i == 3 { // idle is the 4th value
			idle = v
		}
	}
	return cpuSample{idle: idle, total: total}, true
}

// ReadMemory reports host-wide memory usage on Linux (from /proc/meminfo),
// or this process's own memory elsewhere.
func ReadMemory() MemReading {
	if m, ok := readProcMeminfo(); ok {
		return m
	}

	var rt runtime.MemStats
	runtime.ReadMemStats(&rt)
	return MemReading{SystemWide: false, UsedBytes: rt.Sys}
}

func readProcMeminfo() (MemReading, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemReading{}, false
	}
	defer f.Close()

	values := make(map[string]uint64, 4)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Lines look like "MemTotal:       16333764 kB"
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return MemReading{}, false
		}
		values[key] = v * 1024 // kB -> bytes
	}

	total, hasTotal := values["MemTotal"]
	avail, hasAvail := values["MemAvailable"]
	if !hasTotal || !hasAvail || total == 0 {
		return MemReading{}, false
	}

	used := total - avail
	return MemReading{
		SystemWide: true,
		UsedBytes:  used,
		TotalBytes: total,
		Percent:    round1(float64(used) / float64(total) * 100),
	}, true
}

// round1 rounds to one decimal place. Defined in analytics.go (same package).

// processStart is set once, when the package is first loaded — effectively
// when the server process starts — so Uptime can be computed without any
// extra wiring in main.go.
var processStart = time.Now()

func Uptime() time.Duration {
	return time.Since(processStart)
}
