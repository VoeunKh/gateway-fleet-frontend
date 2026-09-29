package domain

import (
	"fmt"
	"strconv"
)

// DeviceState is the update state of one device within a rollout.
type DeviceState string

// Device update states.
const (
	StQueued      DeviceState = "queued"
	StDownloading DeviceState = "downloading"
	StVerifying   DeviceState = "verifying"
	StFlashing    DeviceState = "flashing"
	StRebooting   DeviceState = "rebooting"
	StChecking    DeviceState = "checking"
	StSuccess     DeviceState = "success"
	StRolledBack  DeviceState = "rolledback"
	StBricked     DeviceState = "bricked"
	StSkipped     DeviceState = "skipped"
	StDeferred    DeviceState = "deferred"
)

// Terminal reports whether no further transitions happen.
//
// sample: const TERMINAL = new Set(['success','rolledback','bricked','skipped','deferred']);
func (s DeviceState) Terminal() bool {
	switch s {
	case StSuccess, StRolledBack, StBricked, StSkipped, StDeferred:
		return true
	}
	return false
}

// Processed reports whether the device actually attempted the update
// (counted in the failure rate).
func (s DeviceState) Processed() bool {
	return s == StSuccess || s == StRolledBack || s == StBricked
}

// RolloutDevice is one device's progress inside a rollout.
type RolloutDevice struct {
	SN    string
	State DeviceState
	Pct   float64 // download progress
	From  string  // firmware before the update
	Note  string
}

var transitions = map[DeviceState][]DeviceState{
	StQueued:      {StDeferred, StSkipped, StDownloading},
	StDownloading: {StDownloading, StVerifying},
	StVerifying:   {StFlashing},
	StFlashing:    {StRebooting},
	StRebooting:   {StChecking},
	StChecking:    {StSuccess, StRolledBack, StBricked},
}

// NextDeviceState validates a reported transition. In production the new state
// comes from agent reports (or engine timeouts), not a timer.
//
// sample: stepDevice() — queued → downloading → verifying → flashing → rebooting → checking
// → success | rolledback | bricked
func NextDeviceState(cur, next DeviceState) (DeviceState, error) {
	for _, s := range transitions[cur] {
		if s == next {
			return next, nil
		}
	}
	return cur, fmt.Errorf("invalid device transition %s → %s", cur, next)
}

// Precheck decides what happens to a queued device when its wave starts.
//
// sample: if (!d.online) x.st='deferred';
// else if (d.tmpFree < fw.sizeMB) { x.st='skipped'; x.note=`/tmp has ${d.tmpFree} MB free, image needs ${fw.sizeMB} MB`; }
// else x.st='downloading';
func Precheck(d Device, fw Firmware) (DeviceState, string) {
	if !d.Online {
		return StDeferred, ""
	}
	if d.TmpFreeMB < fw.SizeMB {
		return StSkipped, fmt.Sprintf("/tmp has %s MB free, image needs %s MB", jsNum(d.TmpFreeMB), jsNum(fw.SizeMB))
	}
	return StDownloading, ""
}

// jsNum formats a float like JavaScript's default Number→string for normal values.
func jsNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
