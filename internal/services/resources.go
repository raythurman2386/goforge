package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/docker/go-units"
)

// nanoCPUsPerCPU is the number of Docker NanoCPUs in one CPU core.
const nanoCPUsPerCPU = 1_000_000_000

// nanoDigits is the number of decimal places one NanoCPU represents.
const nanoDigits = 9

// ParseCPU converts a fractional core count such as "0.25" or "1" to Docker
// NanoCPUs, matching `docker run --cpus`. An empty string means no limit and
// returns 0. Values finer than one NanoCPU are rejected rather than rounded.
func ParseCPU(cpu string) (int64, error) {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return 0, nil
	}
	whole, frac, _ := strings.Cut(cpu, ".")
	frac = strings.TrimRight(frac, "0")
	if (whole == "" && frac == "") || !allDigits(whole) || !allDigits(frac) {
		return 0, fmt.Errorf("invalid cpu %q: want a number of cores such as \"0.5\"", cpu)
	}
	if len(frac) > nanoDigits {
		return 0, fmt.Errorf("invalid cpu %q: finer than one NanoCPU", cpu)
	}
	var cores, nano int64
	if frac != "" {
		// frac has at most nanoDigits digits, so it always fits.
		f, err := strconv.ParseInt(frac+strings.Repeat("0", nanoDigits-len(frac)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid cpu %q: %w", cpu, err)
		}
		nano = f
	}
	if whole != "" {
		w, err := strconv.ParseInt(whole, 10, 64)
		if err != nil || w > (math.MaxInt64-nano)/nanoCPUsPerCPU {
			return 0, fmt.Errorf("invalid cpu %q: too large", cpu)
		}
		cores = w
	}
	return cores*nanoCPUsPerCPU + nano, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseMemory converts a Docker memory size such as "256m", "512MB", or "1g"
// to bytes, matching `docker run --memory` (binary units, case-insensitive).
// An empty string means no limit and returns 0.
func ParseMemory(memory string) (int64, error) {
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return 0, nil
	}
	b, err := units.RAMInBytes(memory)
	if err != nil {
		return 0, fmt.Errorf("invalid memory %q: %w", memory, err)
	}
	if b < 0 {
		return 0, fmt.Errorf("invalid memory %q: must not be negative", memory)
	}
	return b, nil
}

// Limits returns the Docker NanoCPUs and memory bytes for r. Unset fields are 0,
// which Docker treats as unlimited.
func (r ResourceConfig) Limits() (nanoCPUs, memory int64, err error) {
	if nanoCPUs, err = ParseCPU(r.CPU); err != nil {
		return 0, 0, err
	}
	if memory, err = ParseMemory(r.Memory); err != nil {
		return 0, 0, err
	}
	return nanoCPUs, memory, nil
}
