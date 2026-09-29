package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func kinds(cs []AlertCondition) []AlertKind {
	var out []AlertKind
	for _, c := range cs {
		out = append(out, c.Kind)
	}
	return out
}

func alertKinds(as []Alert) []AlertKind {
	var out []AlertKind
	for _, a := range as {
		out = append(out, a.Kind)
	}
	return out
}

func TestEvaluateAlerts(t *testing.T) {
	now := t0
	fine := Device{SN: "S1", Online: true, Temp: 50, RAM: 40, LastSeen: now}
	open := func(k AlertKind) Alert { return Alert{SN: "S1", Kind: k, State: AlertOpen} }
	tests := []struct {
		name     string
		mod      func(*Device)
		existing []Alert
		open     []AlertKind
		resolve  []AlertKind
	}{
		{"healthy nothing", func(*Device) {}, nil, nil, nil},
		{"offline 9 min no alert", func(d *Device) { d.Online = false; d.LastSeen = now.Add(-9 * time.Minute) }, nil, nil, nil},
		{"offline exactly 10 min no alert", func(d *Device) { d.Online = false; d.LastSeen = now.Add(-10 * time.Minute) }, nil, nil, nil},
		{"offline 11 min critical", func(d *Device) { d.Online = false; d.LastSeen = now.Add(-11 * time.Minute) }, nil,
			[]AlertKind{AlertOffline}, nil},
		{"offline bricked no offline alert", func(d *Device) {
			d.Online = false
			d.Bricked = true
			d.LastSeen = now.Add(-time.Hour)
		}, nil, nil, nil},
		{"offline alert kept while still offline <10m", func(d *Device) { d.Online = false; d.LastSeen = now }, []Alert{open(AlertOffline)}, nil, nil},
		{"offline alert resolved when online", func(*Device) {}, []Alert{open(AlertOffline)}, nil, []AlertKind{AlertOffline}},
		{"temp 80 critical", func(d *Device) { d.Temp = 80 }, nil, []AlertKind{AlertTemp}, nil},
		{"temp dedupe", func(d *Device) { d.Temp = 85 }, []Alert{open(AlertTemp)}, nil, nil},
		{"temp dedupe while acked", func(d *Device) { d.Temp = 85 }, []Alert{{Kind: AlertTemp, State: AlertAcked}}, nil, nil},
		{"temp resolved when cool", func(*Device) {}, []Alert{{Kind: AlertTemp, State: AlertAcked}}, nil, []AlertKind{AlertTemp}},
		{"temp resolved when offline", func(d *Device) { d.Online = false; d.Temp = 90 }, []Alert{open(AlertTemp)}, nil, []AlertKind{AlertTemp}},
		{"resolved alert does not dedupe", func(d *Device) { d.Temp = 85 }, []Alert{{Kind: AlertTemp, State: AlertResolved}},
			[]AlertKind{AlertTemp}, nil},
		{"ram 90 warning", func(d *Device) { d.RAM = 90 }, nil, []AlertKind{AlertRAM}, nil},
		{"ram 89 none", func(d *Device) { d.RAM = 89 }, []Alert{open(AlertRAM)}, nil, []AlertKind{AlertRAM}},
		{"bricked kept while bricked", func(d *Device) { d.Online = false; d.Bricked = true }, []Alert{open(AlertBricked)}, nil, nil},
		{"bricked resolved when recovered", func(*Device) {}, []Alert{open(AlertBricked)}, nil, []AlertKind{AlertBricked}},
		{"temp and ram together", func(d *Device) { d.Temp = 81; d.RAM = 95 }, nil, []AlertKind{AlertTemp, AlertRAM}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := fine
			tc.mod(&d)
			got := EvaluateAlerts(d, now, tc.existing)
			if !reflect.DeepEqual(kinds(got.Open), tc.open) || !reflect.DeepEqual(alertKinds(got.Resolve), tc.resolve) {
				t.Errorf("open=%v resolve=%v", kinds(got.Open), alertKinds(got.Resolve))
			}
		})
	}
}

func TestAlertMessages(t *testing.T) {
	d := Device{Online: true, Temp: 81.5, RAM: 93, LastSeen: t0}
	got := EvaluateAlerts(d, t0, nil).Open
	want := []AlertCondition{
		{AlertTemp, SevCritical, "Temperature 81.5 °C (limit 80 °C)"},
		{AlertRAM, SevWarning, "RAM use 93% (limit 90%)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	off := EvaluateAlerts(Device{LastSeen: t0.Add(-time.Hour)}, t0, nil).Open
	if len(off) != 1 || off[0] != (AlertCondition{AlertOffline, SevCritical, "Offline for more than 10 minutes"}) {
		t.Errorf("offline: %+v", off)
	}
	if b := BrickedAlert("1.3.0"); b.Severity != SevCritical || b.Message != "No response after update to 1.3.0. Needs on-site recovery." {
		t.Errorf("bricked: %+v", b)
	}
}

func TestAlertLifecycle(t *testing.T) {
	a := Alert{State: AlertOpen}
	if err := AckAlert(&a, "Admin"); err != nil || a.State != AlertAcked || a.AckedBy != "Admin" {
		t.Fatalf("ack: %v %+v", err, a)
	}
	if err := AckAlert(&a, "Admin"); !errors.Is(err, ErrAlertNotOpen) {
		t.Fatalf("double ack: %v", err)
	}
	ResolveAlert(&a, t0)
	if a.State != AlertResolved || !a.ResolvedAt.Equal(t0) {
		t.Fatalf("resolve: %+v", a)
	}
	ResolveAlert(&a, t0.Add(time.Hour)) // idempotent
	if !a.ResolvedAt.Equal(t0) {
		t.Fatal("resolve should keep first time")
	}
	if err := AckAlert(&a, "Admin"); !errors.Is(err, ErrAlertNotOpen) {
		t.Fatal("ack resolved should fail")
	}
	tests := []struct {
		a    Alert
		at   time.Time
		want bool
	}{
		{a, t0.Add(89 * 24 * time.Hour), false},
		{a, t0.Add(91 * 24 * time.Hour), true},
		{Alert{State: AlertOpen}, t0.Add(1000 * 24 * time.Hour), false},
	}
	for i, tc := range tests {
		if AlertExpired(tc.a, tc.at) != tc.want {
			t.Errorf("case %d", i)
		}
	}
}
