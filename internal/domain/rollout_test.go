package domain

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// mkRollout builds a running rollout with the given wave ends and device states.
func mkRollout(counts []int, states ...DeviceState) Rollout {
	r := Rollout{ID: "R-001", Threshold: 10, Counts: counts, State: RolloutRunning}
	for _, s := range states {
		r.Devices = append(r.Devices, RolloutDevice{SN: "x", State: s})
	}
	return r
}

func TestNewRollout(t *testing.T) {
	plan := Plan{Targets: []string{"a", "b"}, Counts: []int{1, 2}}
	devs := map[string]Device{"a": {FW: "1.0"}, "b": {FW: "1.1"}}
	r := NewRollout("R-007", "GW-100", "1.3.0", 0, plan, devs, "Admin", t0)
	if r.Threshold != DefaultThreshold || r.State != RolloutRunning || len(r.Devices) != 2 ||
		r.Devices[1].From != "1.1" || r.Devices[0].State != StQueued || !r.CreatedAt.Equal(t0) {
		t.Errorf("unexpected rollout %+v", r)
	}
	if r2 := NewRollout("R-8", "m", "f", 25, plan, devs, "", t0); r2.Threshold != 25 {
		t.Errorf("threshold %v", r2.Threshold)
	}
	if !RolloutRunning.Active() || !RolloutSoaking.Active() || !RolloutPaused.Active() ||
		RolloutCompleted.Active() || RolloutAborted.Active() {
		t.Error("Active() wrong")
	}
}

func TestWaveDevices(t *testing.T) {
	r := mkRollout([]int{1, 3}, StQueued, StQueued, StQueued)
	if len(r.WaveDevices(0)) != 1 || len(r.WaveDevices(1)) != 2 || r.WaveDevices(2) != nil || r.WaveDevices(-1) != nil {
		t.Error("wave slicing wrong")
	}
	r.WaveDevices(1)[0].State = StSuccess
	if r.Devices[1].State != StSuccess {
		t.Error("wave slice should alias rollout devices")
	}
	empty := Rollout{}
	if b, p, rate := empty.FailureRate(); b != 0 || p != 0 || rate != 0 {
		t.Error("empty failure rate")
	}
}

func TestEvaluateWave(t *testing.T) {
	soak := 5 * time.Minute
	tests := []struct {
		name     string
		r        Rollout
		changed  bool
		state    RolloutState
		reason   string
		wave     int
		override bool
	}{
		{"wave busy", mkRollout([]int{1, 2}, StDownloading, StQueued), false, RolloutRunning, "", 0, false},
		{"not running", func() Rollout { r := mkRollout([]int{1}, StSuccess); r.State = RolloutPaused; return r }(),
			false, RolloutPaused, "", 0, false},
		{"wave ok → soaking", mkRollout([]int{1, 2}, StSuccess, StQueued), true, RolloutSoaking, "", 0, false},
		{"last wave ok → completed", mkRollout([]int{2}, StSuccess, StSuccess), true, RolloutCompleted, "", 0, false},
		{"above threshold → paused", mkRollout([]int{1, 2}, StRolledBack, StQueued), true, RolloutPaused,
			"Auto-paused after wave 1: 1 of 1 updated gateways failed (100%, limit 10%).", 0, false},
		{"rate counts all waves so far", func() Rollout {
			r := mkRollout([]int{2, 5, 6}, StBricked, StSuccess, StSuccess, StSuccess, StSuccess, StQueued)
			r.Wave = 1
			return r
		}(), true, RolloutPaused, "Auto-paused after wave 2: 1 of 5 updated gateways failed (20%, limit 10%).", 1, false},
		{"threshold exactly equal → not paused", func() Rollout {
			r := mkRollout([]int{10, 11}, StRolledBack, StSuccess, StSuccess, StSuccess, StSuccess,
				StSuccess, StSuccess, StSuccess, StSuccess, StSuccess, StQueued)
			return r
		}(), true, RolloutSoaking, "", 0, false},
		{"skipped/deferred not counted", mkRollout([]int{3, 4}, StSkipped, StDeferred, StSuccess, StQueued),
			true, RolloutSoaking, "", 0, false},
		{"all offline (all deferred) → soaking", mkRollout([]int{2, 3}, StDeferred, StDeferred, StQueued),
			true, RolloutSoaking, "", 0, false},
		{"override skips check and is cleared", func() Rollout {
			r := mkRollout([]int{1, 2, 3}, StBricked, StBricked, StQueued)
			r.Wave, r.Override = 1, true
			return r
		}(), true, RolloutSoaking, "", 1, false},
		{"rounding matches toFixed(0)", func() Rollout {
			// 1 of 8 = 12.5% → "13"
			r := mkRollout([]int{8, 9}, StRolledBack, StSuccess, StSuccess, StSuccess, StSuccess, StSuccess, StSuccess, StSuccess, StQueued)
			r.Threshold = 12.4
			return r
		}(), true, RolloutPaused, "Auto-paused after wave 1: 1 of 8 updated gateways failed (13%, limit 12.4%).", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			changed := EvaluateWave(&r, t0, soak)
			if changed != tc.changed || r.State != tc.state || r.Reason != tc.reason || r.Wave != tc.wave || r.Override != tc.override {
				t.Errorf("changed=%v state=%s reason=%q wave=%d override=%v", changed, r.State, r.Reason, r.Wave, r.Override)
			}
			if r.State == RolloutSoaking && !r.SoakUntil.Equal(t0.Add(soak)) {
				t.Errorf("soak until %v", r.SoakUntil)
			}
			if r.State == RolloutCompleted && !r.FinishedAt.Equal(t0) {
				t.Errorf("finished at %v", r.FinishedAt)
			}
		})
	}
}

func TestAdvanceSoak(t *testing.T) {
	r := mkRollout([]int{1, 2}, StSuccess, StQueued)
	r.State, r.SoakUntil = RolloutSoaking, t0.Add(time.Minute)
	if AdvanceSoak(&r, t0) || r.State != RolloutSoaking {
		t.Fatal("advanced before soak ended")
	}
	if !AdvanceSoak(&r, t0.Add(time.Minute)) || r.State != RolloutRunning || r.Wave != 1 || !r.SoakUntil.IsZero() {
		t.Fatalf("not advanced: %+v", r)
	}
	if AdvanceSoak(&r, t0.Add(time.Hour)) {
		t.Fatal("advanced a running rollout")
	}
}

func TestApplyRolloutAction(t *testing.T) {
	with := func(r Rollout, st RolloutState, wave int) Rollout { r.State, r.Wave = st, wave; return r }
	tests := []struct {
		name     string
		r        Rollout
		act      RolloutAction
		err      bool
		state    RolloutState
		wave     int
		override bool
		reason   string
	}{
		{"pause running", mkRollout([]int{1}, StQueued), ActPause, false, RolloutPaused, 0, false, "Paused by Admin."},
		{"pause soaking", with(mkRollout([]int{1, 2}, StSuccess, StQueued), RolloutSoaking, 0), ActPause, false, RolloutPaused, 0, false, "Paused by Admin."},
		{"pause paused", with(mkRollout([]int{1}, StQueued), RolloutPaused, 0), ActPause, true, RolloutPaused, 0, false, ""},
		{"resume mid-wave", with(mkRollout([]int{2}, StSuccess, StDownloading), RolloutPaused, 0), ActResume, false, RolloutRunning, 0, false, ""},
		{"resume after wave → next wave with override", with(mkRollout([]int{1, 2}, StBricked, StQueued), RolloutPaused, 0),
			ActResume, false, RolloutRunning, 1, true, ""},
		{"resume after last wave → completed", with(mkRollout([]int{1, 2}, StBricked, StBricked), RolloutPaused, 1),
			ActResume, false, RolloutCompleted, 1, false, ""},
		{"resume running", mkRollout([]int{1}, StQueued), ActResume, true, RolloutRunning, 0, false, ""},
		{"abort running", mkRollout([]int{1, 3}, StSuccess, StQueued, StDownloading), ActAbort, false, RolloutAborted, 0, false,
			"Aborted by Admin. Gateways not yet started stay on their current firmware."},
		{"abort paused", with(mkRollout([]int{1}, StQueued), RolloutPaused, 0), ActAbort, false, RolloutAborted, 0, false,
			"Aborted by Admin. Gateways not yet started stay on their current firmware."},
		{"abort completed", with(mkRollout([]int{1}, StSuccess), RolloutCompleted, 0), ActAbort, true, RolloutCompleted, 0, false, ""},
		{"abort aborted", with(mkRollout([]int{1}, StDeferred), RolloutAborted, 0), ActAbort, true, RolloutAborted, 0, false, ""},
		{"unknown action", mkRollout([]int{1}, StQueued), "explode", true, RolloutRunning, 0, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			r.Reason = ""
			err := ApplyRolloutAction(&r, tc.act, "Admin", t0)
			if tc.err != (err != nil) || (err != nil && !errors.Is(err, ErrActionNotAllowed)) {
				t.Fatalf("err=%v", err)
			}
			if r.State != tc.state || r.Wave != tc.wave || r.Override != tc.override || r.Reason != tc.reason {
				t.Errorf("state=%s wave=%d override=%v reason=%q", r.State, r.Wave, r.Override, r.Reason)
			}
		})
	}
	r := mkRollout([]int{1, 3}, StSuccess, StQueued, StDownloading)
	_ = ApplyRolloutAction(&r, ActAbort, "Admin", t0)
	if r.Devices[0].State != StSuccess || r.Devices[1].State != StDeferred || r.Devices[2].State != StDownloading {
		t.Errorf("abort must defer only queued devices: %+v", r.Devices)
	}
}

// TestRolloutScenario walks a full rollout: wave, soak, failures, auto-pause, resume with override.
func TestRolloutScenario(t *testing.T) {
	r := mkRollout([]int{1, 2, 4}, StQueued, StQueued, StQueued, StQueued)
	now := t0
	finish := func(i int, s DeviceState) { r.Devices[i].State = s }

	finish(0, StSuccess)
	EvaluateWave(&r, now, DefaultSoak)
	if r.State != RolloutSoaking {
		t.Fatalf("after wave 1: %s", r.State)
	}
	now = now.Add(DefaultSoak)
	AdvanceSoak(&r, now)
	finish(1, StRolledBack)
	EvaluateWave(&r, now, DefaultSoak)
	if r.State != RolloutPaused || r.Reason != "Auto-paused after wave 2: 1 of 2 updated gateways failed (50%, limit 10%)." {
		t.Fatalf("expected auto-pause: %s %q", r.State, r.Reason)
	}
	if err := ApplyRolloutAction(&r, ActResume, "Admin", now); err != nil || r.Wave != 2 || !r.Override {
		t.Fatalf("resume: %v %+v", err, r)
	}
	finish(2, StBricked)
	finish(3, StSuccess)
	EvaluateWave(&r, now, DefaultSoak)
	if r.State != RolloutCompleted {
		t.Fatalf("override should let last wave complete: %s %q", r.State, r.Reason)
	}
}
