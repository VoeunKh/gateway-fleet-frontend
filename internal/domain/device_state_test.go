package domain

import "testing"

func TestTerminalProcessed(t *testing.T) {
	tests := []struct {
		s               DeviceState
		terminal, count bool
	}{
		{StQueued, false, false}, {StDownloading, false, false}, {StVerifying, false, false},
		{StFlashing, false, false}, {StRebooting, false, false}, {StChecking, false, false},
		{StSuccess, true, true}, {StRolledBack, true, true}, {StBricked, true, true},
		{StSkipped, true, false}, {StDeferred, true, false},
	}
	for _, tc := range tests {
		if tc.s.Terminal() != tc.terminal || tc.s.Processed() != tc.count {
			t.Errorf("%s: terminal=%v processed=%v", tc.s, tc.s.Terminal(), tc.s.Processed())
		}
	}
}

func TestNextDeviceState(t *testing.T) {
	tests := []struct {
		from, to DeviceState
		ok       bool
	}{
		{StQueued, StDownloading, true},
		{StQueued, StDeferred, true},
		{StQueued, StSkipped, true},
		{StDownloading, StDownloading, true},
		{StDownloading, StVerifying, true},
		{StVerifying, StFlashing, true},
		{StFlashing, StRebooting, true},
		{StRebooting, StChecking, true},
		{StChecking, StSuccess, true},
		{StChecking, StRolledBack, true},
		{StChecking, StBricked, true},
		{StQueued, StFlashing, false},
		{StDownloading, StSuccess, false},
		{StSuccess, StQueued, false},
		{StBricked, StSuccess, false},
		{StDeferred, StDownloading, false},
	}
	for _, tc := range tests {
		got, err := NextDeviceState(tc.from, tc.to)
		if tc.ok && (err != nil || got != tc.to) {
			t.Errorf("%s→%s: unexpected error %v", tc.from, tc.to, err)
		}
		if !tc.ok && (err == nil || got != tc.from) {
			t.Errorf("%s→%s: expected error, got %s", tc.from, tc.to, got)
		}
	}
}

func TestPrecheck(t *testing.T) {
	fw := Firmware{SizeMB: 12}
	tests := []struct {
		name  string
		d     Device
		state DeviceState
		note  string
	}{
		{"offline deferred", Device{Online: false, TmpFreeMB: 100}, StDeferred, ""},
		{"low tmp skipped", Device{Online: true, TmpFreeMB: 7}, StSkipped, "/tmp has 7 MB free, image needs 12 MB"},
		{"fractional", Device{Online: true, TmpFreeMB: 11.5}, StSkipped, "/tmp has 11.5 MB free, image needs 12 MB"},
		{"exact fits", Device{Online: true, TmpFreeMB: 12}, StDownloading, ""},
	}
	for _, tc := range tests {
		st, note := Precheck(tc.d, fw)
		if st != tc.state || note != tc.note {
			t.Errorf("%s: got %s %q", tc.name, st, note)
		}
	}
}
