package seed

import (
	"strings"
	"testing"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
)

var now = time.UnixMilli(1790000000000).UTC()

// Golden values were produced by running the sample's own initData() in Node with
// Date.now() = 1790000000000; the full dump matched field for field.
func TestGenerateMatchesSample(t *testing.T) {
	d := Generate(now)
	if len(d.Models) != 3 || len(d.Sites) != 6 || len(d.Firmware) != 9 || len(d.Packages) != 5 || len(d.Configs) != 6 || len(d.Devices) != 60 {
		t.Fatalf("sizes: %d models %d sites %d fw %d pkgs %d cfgs %d devices",
			len(d.Models), len(d.Sites), len(d.Firmware), len(d.Packages), len(d.Configs), len(d.Devices))
	}
	if got := d.Devices[0].SN; got != "GW100-2431-00001" {
		t.Errorf("first sn %s", got)
	}
	if got := d.Devices[59].SN; got != "GW300-2438-00220" {
		t.Errorf("last sn %s", got)
	}
	if got := d.Firmware[0].SHA256[:16]; got != "6054EA282ED50999" {
		t.Errorf("sha %s", got)
	}
	if got := d.Devices[20].Interfaces[1].Ident; got != "IMEI 080764246623510, ICCID 8928394032859425682" {
		t.Errorf("lte ident %s", got)
	}
	last := d.Devices[59]
	if last.TmpFreeMB != 91 || *last.RSSI != -63 || last.Temps[0] != 50 || last.Temps[4] != 46 {
		t.Errorf("device 59: tmp=%v rssi=%v temps=%v", last.TmpFreeMB, *last.RSSI, last.Temps[:5])
	}
	var online, cfg1, fw12 int
	var offline, hot []string
	for _, x := range d.Devices {
		if x.Online {
			online++
			if x.Temp >= 80 {
				hot = append(hot, x.SN)
			}
		} else {
			offline = append(offline, x.SN)
		}
		if x.CfgVer == 1 {
			cfg1++
		}
		if x.FW == "1.2.0" {
			fw12++
		}
	}
	if online != 51 || cfg1 != 12 || fw12 != 30 {
		t.Errorf("online=%d cfg1=%d fw1.2.0=%d", online, cfg1, fw12)
	}
	if strings.Join(hot, ",") != "GW200-2519-00113,GW200-2507-00118" {
		t.Errorf("hot %v", hot)
	}
	if len(offline) != 9 || offline[0] != "GW100-2431-00006" {
		t.Errorf("offline %v", offline)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	a, b := Generate(now), Generate(now)
	for i := range a.Devices {
		if a.Devices[i].SN != b.Devices[i].SN || a.Devices[i].Temp != b.Devices[i].Temp {
			t.Fatal("not deterministic")
		}
	}
}

func TestSeedConfigsAreValid(t *testing.T) {
	d := Generate(now)
	for _, c := range d.Configs {
		if err := domain.ValidateUCI(c.Text); err != nil {
			t.Errorf("%s v%d: %v", c.Model, c.V, err)
		}
	}
	gw200v2 := d.Configs[3].Text
	for _, want := range []string{"option heartbeat '30'", "config timeserver 'ntp'", "config lora", "{{secret.sim_pin}}", "config modbus 'rtu'"} {
		if !strings.Contains(gw200v2, want) {
			t.Errorf("GW-200 v2 missing %q", want)
		}
	}
	if strings.Contains(d.Configs[0].Text, "config interface 'wwan'") {
		t.Error("GW-100 has no LTE")
	}
}

func TestJSHelpers(t *testing.T) {
	tests := []struct{ in, round, fixed float64 }{
		{2.5, 3, 2.5}, {-2.5, -2, -2.5}, {-0.4, 0, -0.4}, {7.9, 8, 7.9}, {1.25, 1, 1.2}, {33.36, 33, 33.4},
	}
	for _, tc := range tests {
		if jsRound(tc.in) != tc.round || toFixed1(tc.in) != tc.fixed {
			t.Errorf("%v: round=%v fixed=%v", tc.in, jsRound(tc.in), toFixed1(tc.in))
		}
	}
	if leftPad(7, 5) != "00007" || leftPad(123456, 5) != "123456" {
		t.Error("leftPad")
	}
}
