//go:build linux

package inference

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
)

type platformSampler struct {
	mu        sync.Mutex
	prevIdle  uint64
	prevTotal uint64
}

func newPlatformSampler() platformSampler { return platformSampler{} }

func (p *platformSampler) sample() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Snapshot{}
	if f, err := os.Open("/proc/stat"); err == nil {
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 5 && fields[0] == "cpu" {
				var total uint64
				vals := make([]uint64, 0, len(fields)-1)
				for _, v := range fields[1:] {
					n, _ := strconv.ParseUint(v, 10, 64)
					vals = append(vals, n)
					total += n
				}
				idle := vals[3]
				if len(vals) > 4 {
					idle += vals[4]
				}
				if p.prevTotal > 0 && total > p.prevTotal {
					dt, di := total-p.prevTotal, idle-p.prevIdle
					s.CPUPercent = 100 * float64(dt-di) / float64(dt)
				}
				p.prevTotal, p.prevIdle = total, idle
			}
		}
		_ = f.Close()
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		vals := map[string]int{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 {
				n, _ := strconv.Atoi(fields[1])
				vals[strings.TrimSuffix(fields[0], ":")] = n / 1024
			}
		}
		_ = f.Close()
		total, avail := vals["MemTotal"], vals["MemAvailable"]
		s.RAMFreeMB = avail
		s.RAMUsedMB = total - avail
	}
	return s
}
