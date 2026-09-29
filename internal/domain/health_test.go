package domain

import "testing"

func TestDeviceHealth(t *testing.T) {
	ok := Device{Online: true, Temp: 50, RAM: 40, TmpFreeMB: 20}
	tests := []struct {
		name string
		mod  func(*Device)
		want Health
	}{
		{"healthy", func(*Device) {}, Healthy},
		{"offline wins over critical", func(d *Device) { d.Online = false; d.Temp = 99 }, Offline},
		{"temp 80 critical", func(d *Device) { d.Temp = 80 }, Critical},
		{"ram 90 critical", func(d *Device) { d.RAM = 90 }, Critical},
		{"temp 79.9 warning", func(d *Device) { d.Temp = 79.9 }, Warning},
		{"temp 72 warning", func(d *Device) { d.Temp = 72 }, Warning},
		{"temp 71.9 healthy", func(d *Device) { d.Temp = 71.9 }, Healthy},
		{"ram 80 warning", func(d *Device) { d.RAM = 80 }, Warning},
		{"ram 79 healthy", func(d *Device) { d.RAM = 79 }, Healthy},
		{"tmp 7.9 warning", func(d *Device) { d.TmpFreeMB = 7.9 }, Warning},
		{"tmp 8 healthy", func(d *Device) { d.TmpFreeMB = 8 }, Healthy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := ok
			tc.mod(&d)
			if got := DeviceHealth(d); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDrift(t *testing.T) {
	tests := []struct {
		have, want int
		drift      bool
	}{{2, 2, false}, {1, 2, true}, {3, 2, true}}
	for _, tc := range tests {
		if got := Drift(Device{CfgVer: tc.have}, tc.want); got != tc.drift {
			t.Errorf("cfg %d desired %d: got %v", tc.have, tc.want, got)
		}
	}
}
