package domain

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestParseWaves(t *testing.T) {
	const bad = "Waves must be percentages ending in 100, for example 1,10,50,100."
	tests := []struct {
		in   string
		want []float64
		err  string
	}{
		{"1,10,50,100", []float64{1, 10, 50, 100}, ""},
		{" 5, 100", []float64{5, 100}, ""},
		{"10%,100%", []float64{10, 100}, ""},
		{"0,-5,abc,150,100", []float64{100}, ""}, // invalid entries are dropped, like the sample
		{"100", []float64{100}, ""},
		{"0.5,100", []float64{0.5, 100}, ""},
		{"1,10,50", nil, bad},
		{"100,50", nil, bad},
		{"", nil, bad},
		{"abc", nil, bad},
	}
	for _, tc := range tests {
		got, err := ParseWaves(tc.in)
		if tc.err != "" {
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Msg != tc.err {
				t.Errorf("%q: err=%v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestWaveCounts(t *testing.T) {
	tests := []struct {
		n    int
		pcts []float64
		want []int
	}{
		{20, []float64{1, 10, 50, 100}, []int{1, 2, 10, 20}},
		{60, []float64{1, 10, 50, 100}, []int{1, 6, 30, 60}},
		{3, []float64{50, 100}, []int{2, 3}},
		{1, []float64{1, 10, 50, 100}, []int{1}},           // duplicates dropped
		{2, []float64{1, 10, 50, 100}, []int{1, 2}},        // clamp then dedupe
		{5, []float64{10, 20, 30, 100}, []int{1, 2, 3, 5}}, // prev+1 forces growth
		{7, []float64{100}, []int{7}},
	}
	for _, tc := range tests {
		if got := WaveCounts(tc.n, tc.pcts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("n=%d %v: got %v, want %v", tc.n, tc.pcts, got, tc.want)
		}
	}
}

func fleet(model string, n int, fw string) []Device {
	ds := make([]Device, n)
	for i := range ds {
		ds[i] = Device{SN: fmt.Sprintf("%s-%02d", model, i), Model: model, FW: fw, Online: true, Temp: 50, RAM: 40, TmpFreeMB: 20}
	}
	return ds
}

func TestPlanWaves(t *testing.T) {
	fw := &Firmware{Model: "GW-100", Version: "1.3.0", SizeMB: 12}
	base := func() PlanInput {
		ds := fleet("GW-100", 5, "1.2.0")
		ds = append(ds, fleet("GW-200", 2, "1.2.0")...)
		return PlanInput{Model: "GW-100", TargetFW: "1.3.0", Firmware: fw, Waves: "1,10,50,100", Devices: ds}
	}
	tests := []struct {
		name    string
		mod     func(*PlanInput)
		targets []string
		counts  []int
		err     string
	}{
		{"ok", func(*PlanInput) {}, []string{"GW-100-00", "GW-100-01", "GW-100-02", "GW-100-03", "GW-100-04"}, []int{1, 2, 3, 5}, ""},
		{"unknown fw", func(in *PlanInput) { in.Firmware = nil }, nil, nil, "Choose a firmware version that is not blocked."},
		{"blocked fw", func(in *PlanInput) { in.Firmware = &Firmware{Blocked: true} }, nil, nil, "Choose a firmware version that is not blocked."},
		{"bad waves", func(in *PlanInput) { in.Waves = "1,10" }, nil, nil, "Waves must be percentages ending in 100, for example 1,10,50,100."},
		{"all on target", func(in *PlanInput) {
			for i := range in.Devices {
				in.Devices[i].FW = "1.3.0"
			}
		}, nil, nil, "All GW-100 gateways already run 1.3.0."},
		{"active rollout", func(in *PlanInput) { in.ActiveForModel = true }, nil, nil, "A rollout for GW-100 is already active. Finish or abort it first."},
		{"skip bricked and up to date, healthy first stable", func(in *PlanInput) {
			in.Devices[0].Bricked = true
			in.Devices[1].Temp = 85 // critical
			in.Devices[2].FW = "1.3.0"
			in.Devices[3].Online = false
		}, []string{"GW-100-04", "GW-100-01", "GW-100-03"}, []int{1, 2, 3}, ""},
		{"all offline still planned", func(in *PlanInput) {
			for i := range in.Devices {
				in.Devices[i].Online = false
			}
			in.Waves = "50,100"
		}, []string{"GW-100-00", "GW-100-01", "GW-100-02", "GW-100-03", "GW-100-04"}, []int{3, 5}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mod(&in)
			p, err := PlanWaves(in)
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err=%v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Targets, tc.targets) || !reflect.DeepEqual(p.Counts, tc.counts) {
				t.Errorf("got %v %v, want %v %v", p.Targets, p.Counts, tc.targets, tc.counts)
			}
		})
	}
}
