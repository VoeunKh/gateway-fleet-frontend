package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// RolloutState is the state of a staged rollout.
type RolloutState string

// Rollout states.
const (
	RolloutRunning   RolloutState = "running"
	RolloutSoaking   RolloutState = "soaking"
	RolloutPaused    RolloutState = "paused"
	RolloutCompleted RolloutState = "completed"
	RolloutAborted   RolloutState = "aborted"
)

// Active reports whether the rollout blocks another rollout for the same model.
//
// sample: ['running','soaking','paused'].includes(r.state)
func (s RolloutState) Active() bool {
	return s == RolloutRunning || s == RolloutSoaking || s == RolloutPaused
}

// DefaultSoak is the wait between waves.
const DefaultSoak = 5 * time.Minute

// Rollout is a staged firmware rollout for one model.
type Rollout struct {
	ID         string // human id, R-001
	Model      string
	FW         string
	Threshold  float64 // failure-rate limit in %
	Counts     []int   // cumulative wave ends
	Wave       int     // current wave index
	State      RolloutState
	Reason     string
	Override   bool // set by resume after an auto-pause; skips the next threshold check
	SoakUntil  time.Time
	CreatedBy  string
	CreatedAt  time.Time
	FinishedAt time.Time
	Devices    []RolloutDevice
}

// NewRollout builds a running rollout from a plan. devices must contain every target.
func NewRollout(id, model, fw string, threshold float64, plan Plan, devices map[string]Device, by string, now time.Time) Rollout {
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	r := Rollout{ID: id, Model: model, FW: fw, Threshold: threshold, Counts: plan.Counts,
		State: RolloutRunning, CreatedBy: by, CreatedAt: now}
	for _, sn := range plan.Targets {
		r.Devices = append(r.Devices, RolloutDevice{SN: sn, State: StQueued, From: devices[sn].FW})
	}
	return r
}

// WaveDevices returns the devices of wave i (a sub-slice; edits are visible in r).
//
// sample: waveSlice(r, i){ return r.devs.slice(i===0?0:r.counts[i-1], r.counts[i]); }
func (r *Rollout) WaveDevices(i int) []RolloutDevice {
	if i < 0 || i >= len(r.Counts) {
		return nil
	}
	start := 0
	if i > 0 {
		start = r.Counts[i-1]
	}
	return r.Devices[start:r.Counts[i]]
}

func (r *Rollout) waveDone(i int) bool {
	for _, x := range r.WaveDevices(i) {
		if !x.State.Terminal() {
			return false
		}
	}
	return true
}

func (r *Rollout) lastWave() bool { return r.Wave >= len(r.Counts)-1 }

// FailureRate returns (bad, processed, rate%) over all waves up to the current one.
func (r *Rollout) FailureRate() (bad, processed int, rate float64) {
	if len(r.Counts) == 0 {
		return 0, 0, 0
	}
	for _, x := range r.Devices[:r.Counts[min(r.Wave, len(r.Counts)-1)]] {
		if x.State.Processed() {
			processed++
			if x.State != StSuccess {
				bad++
			}
		}
	}
	if processed > 0 {
		rate = float64(bad) / float64(processed) * 100
	}
	return bad, processed, rate
}

// EvaluateWave advances a running rollout once every device in the current wave
// is terminal. It returns true if r changed.
//
// sample: tickRollout() — if (!r.override && rate > r.thr) paused;
// else if last wave completed; else { r.state='soaking'; r.override=false; }
func EvaluateWave(r *Rollout, now time.Time, soak time.Duration) bool {
	if r.State != RolloutRunning || !r.waveDone(r.Wave) {
		return false
	}
	bad, proc, rate := r.FailureRate()
	switch {
	case !r.Override && rate > r.Threshold:
		r.State = RolloutPaused
		r.Reason = fmt.Sprintf("Auto-paused after wave %d: %d of %d updated gateways failed (%s%%, limit %s%%).",
			r.Wave+1, bad, proc, jsNum(math.Round(rate)), jsNum(r.Threshold))
	case r.lastWave():
		r.State = RolloutCompleted
		r.FinishedAt = now
	default:
		r.State = RolloutSoaking
		r.SoakUntil = now.Add(soak)
		r.Override = false
	}
	return true
}

// AdvanceSoak moves a soaking rollout to the next wave once the soak time passed.
// Using a deadline (not a counter) lets rollouts resume after a server restart.
//
// sample: if (r.state==='soaking'){ if (--r.soak <= 0){ r.wave++; r.state='running'; } }
func AdvanceSoak(r *Rollout, now time.Time) bool {
	if r.State != RolloutSoaking || now.Before(r.SoakUntil) {
		return false
	}
	r.Wave++
	r.State = RolloutRunning
	r.SoakUntil = time.Time{}
	return true
}

// RolloutAction is a user action on a rollout.
type RolloutAction string

// Rollout actions.
const (
	ActPause  RolloutAction = "pause"
	ActResume RolloutAction = "resume"
	ActAbort  RolloutAction = "abort"
)

// ErrActionNotAllowed is returned when an action does not apply in the current state.
var ErrActionNotAllowed = errors.New("action not allowed in current rollout state")

// ApplyRolloutAction applies pause, resume or abort. by is the actor's display name.
//
// sample: rolloutAction(id, act)
func ApplyRolloutAction(r *Rollout, act RolloutAction, by string, now time.Time) error {
	switch {
	case act == ActPause && (r.State == RolloutRunning || r.State == RolloutSoaking):
		r.State = RolloutPaused
		r.Reason = fmt.Sprintf("Paused by %s.", by)
	case act == ActResume && r.State == RolloutPaused:
		waveDone := r.waveDone(r.Wave)
		switch {
		case waveDone && r.lastWave():
			r.State = RolloutCompleted
			r.FinishedAt = now
		case waveDone:
			r.Wave++
			r.State = RolloutRunning
			r.Override = true
		default:
			r.State = RolloutRunning
		}
		r.Reason = ""
		r.SoakUntil = time.Time{}
	case act == ActAbort && r.State != RolloutCompleted && r.State != RolloutAborted:
		r.State = RolloutAborted
		r.Reason = fmt.Sprintf("Aborted by %s. Gateways not yet started stay on their current firmware.", by)
		r.FinishedAt = now
		for i := range r.Devices {
			if r.Devices[i].State == StQueued {
				r.Devices[i].State = StDeferred
			}
		}
	default:
		return fmt.Errorf("%s on %s rollout: %w", act, r.State, ErrActionNotAllowed)
	}
	return nil
}
