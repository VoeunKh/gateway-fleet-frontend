package domain

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// DefaultThreshold is the failure-rate limit (%) used when none is given.
//
// sample: thr:+f.thr || 10
const DefaultThreshold = 10.0

// PlanInput is everything needed to plan a rollout.
type PlanInput struct {
	Model    string
	TargetFW string
	Firmware *Firmware // image for (Model, TargetFW); nil if unknown
	Waves    string    // e.g. "1,10,50,100"
	Devices  []Device  // all devices (filtered by model here)
	// ActiveForModel is true if a running, soaking or paused rollout exists for Model.
	ActiveForModel bool
}

// Plan is the result of PlanWaves.
type Plan struct {
	Targets []string // serials in rollout order (healthy first)
	Counts  []int    // cumulative device count at the end of each wave
}

// ParseWaves parses a comma-separated list of percentages.
//
// sample: f.waves.split(',').map(x => parseFloat(x)).filter(x => x > 0 && x <= 100);
// if (!pcts.length || pcts[pcts.length-1] !== 100) toast('Waves must be percentages ending in 100, ...')
func ParseWaves(s string) ([]float64, error) {
	var pcts []float64
	for _, part := range strings.Split(s, ",") {
		p, ok := parseFloatPrefix(part)
		if ok && p > 0 && p <= 100 {
			pcts = append(pcts, p)
		}
	}
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		return nil, invalid("Waves must be percentages ending in 100, for example 1,10,50,100.")
	}
	return pcts, nil
}

// parseFloatPrefix mimics JS parseFloat: leading whitespace is skipped and the
// longest numeric prefix is parsed ("10%" → 10).
func parseFloatPrefix(s string) (float64, bool) {
	s = strings.TrimLeft(s, " \t\n\r")
	for end := len(s); end > 0; end-- {
		if v, err := strconv.ParseFloat(s[:end], 64); err == nil && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return v, true
		}
	}
	return 0, false
}

// WaveCounts turns percentages into cumulative device counts.
//
// sample: const c = Math.max(prev+1, Math.ceil(targets.length*p/100)); prev = Math.min(c, targets.length);
// uniq = counts.filter((c,i) => i===0 || c > counts[i-1]);
func WaveCounts(n int, pcts []float64) []int {
	counts := make([]int, 0, len(pcts))
	prev := 0
	for _, p := range pcts {
		c := max(prev+1, int(math.Ceil(float64(n)*p/100)))
		prev = min(c, n)
		if len(counts) == 0 || prev > counts[len(counts)-1] {
			counts = append(counts, prev)
		}
	}
	return counts
}

// PlanWaves validates a rollout request and returns target order and wave sizes.
// Checks run in the sample's order so the first error matches it.
func PlanWaves(in PlanInput) (Plan, error) {
	// sample: if (!fw || fw.blocked) toast('Choose a firmware version that is not blocked.')
	if in.Firmware == nil || in.Firmware.Blocked {
		return Plan{}, invalid("Choose a firmware version that is not blocked.")
	}
	pcts, err := ParseWaves(in.Waves)
	if err != nil {
		return Plan{}, err
	}
	// sample: targets = S.devices.filter(d => d.model===f.model && d.fw!==f.fw && !d.bricked)
	var targets []Device
	for _, d := range in.Devices {
		if d.Model == in.Model && d.FW != in.TargetFW && !d.Bricked {
			targets = append(targets, d)
		}
	}
	if len(targets) == 0 {
		return Plan{}, invalid(fmt.Sprintf("All %s gateways already run %s.", in.Model, in.TargetFW))
	}
	if in.ActiveForModel {
		return Plan{}, invalid(fmt.Sprintf("A rollout for %s is already active. Finish or abort it first.", in.Model))
	}
	// sample: healthyFirst = targets.slice().sort((a,b) => (health(a)==='healthy'?0:1) - (health(b)==='healthy'?0:1))
	sort.SliceStable(targets, func(i, j int) bool {
		return DeviceHealth(targets[i]) == Healthy && DeviceHealth(targets[j]) != Healthy
	})
	plan := Plan{Counts: WaveCounts(len(targets), pcts)}
	for _, d := range targets {
		plan.Targets = append(plan.Targets, d.SN)
	}
	return plan, nil
}
