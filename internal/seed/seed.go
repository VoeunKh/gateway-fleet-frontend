// Package seed generates the sample fleet deterministically. It is a 1:1 port of
// initData() in docs/reference/sample-console.html (seed 20260920), so screenshots
// match the sample. It performs no I/O.
package seed

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
)

// Seed is the sample's PRNG seed.
const Seed = 20260920

// Data is the generated fleet.
type Data struct {
	Models   []domain.Model
	Sites    []domain.Site
	Firmware []Firmware
	Packages []domain.Package
	Configs  []domain.ConfigVersion
	Desired  map[string]int // model → desired config version
	Devices  []Device
}

// Firmware adds the simulated failure rate (used by fleetsim only, not stored).
type Firmware struct {
	domain.Firmware
	Released string // YYYY-MM-DD
	FailRate float64
}

// Device adds per-device seed data that lives in other tables.
type Device struct {
	domain.Device
	Interfaces []domain.Interface
	Packages   map[string]string // name → installed version
	Temps      []float64         // 30-point temperature history, oldest first
	History    []Event
}

// Event is a device history entry.
type Event struct {
	At  time.Time
	Msg string
}

type iface struct{ kind, name string }

type modelDef struct {
	domain.Model
	ifaces []iface
}

// sample: const MODELS = [...]
var models = []modelDef{
	{domain.Model{ID: "GW-100", Name: "Indoor LoRa", SoC: "MediaTek MT7621A", Arch: "mipsel_24kc", RAMMB: 128, FlashMB: 16, OS: "OpenWrt 23.05.4"},
		[]iface{{"eth", "lan1"}, {"eth", "wan"}, {"wifi", "wlan0"}, {"lora", "lora0"}}},
	{domain.Model{ID: "GW-200", Name: "Outdoor LTE", SoC: "MediaTek MT7628AN", Arch: "mipsel_24kc", RAMMB: 128, FlashMB: 32, OS: "OpenWrt 23.05.4"},
		[]iface{{"eth", "wan"}, {"lte", "wwan0"}, {"lora", "lora0"}, {"wifi", "wlan0"}, {"rs485", "ttyS1"}}},
	{domain.Model{ID: "GW-300", Name: "Industrial", SoC: "Qualcomm IPQ4019", Arch: "arm_cortex-a7", RAMMB: 256, FlashMB: 128, OS: "OpenWrt 23.05.4"},
		[]iface{{"eth", "lan1"}, {"eth", "lan2"}, {"eth", "lan3"}, {"eth", "wan"}, {"lte", "wwan0"}, {"ble", "hci0"}, {"usb", "usb1"}}},
}

// rng is the sample's LCG.
//
// sample: const rnd = () => (seed = (Math.imul(seed,1664525) + 1013904223) >>> 0) / 4294967296;
type rng struct{ s uint32 }

func (r *rng) next() float64 {
	r.s = r.s*1664525 + 1013904223
	return float64(r.s) / 4294967296
}

func (r *rng) pick(a []string) string { return a[int(math.Floor(r.next()*float64(len(a))))] }

func (r *rng) hex(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(strings.ToUpper(strconv.FormatInt(int64(math.Floor(r.next()*16)), 16)))
	}
	return b.String()
}

func (r *rng) digits(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(strconv.Itoa(int(math.Floor(r.next() * 10))))
	}
	return b.String()
}

func (r *rng) mac() string { return "70:B3:D5:" + r.hex(2) + ":" + r.hex(2) + ":" + r.hex(2) }

// sample: function ifId(t)
func (r *rng) ifID(t string) string {
	switch t {
	case "eth", "wifi", "ble":
		return "MAC " + r.mac()
	case "lte":
		imei := r.digits(15)
		return "IMEI " + imei + ", ICCID 89" + r.digits(17)
	case "lora":
		return "EUI " + r.hex(16)
	case "rs485":
		return "/dev/ttyS1"
	}
	return "USB 2.0 host"
}

// jsRound is Math.round (halves round towards +∞).
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// toFixed1 is +x.toFixed(1).
func toFixed1(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 1, 64), 64)
	return v
}

func hasIface(m modelDef, kind string) bool {
	for _, i := range m.ifaces {
		if i.kind == kind {
			return true
		}
	}
	return false
}

// uciTemplate builds the sample's config template for a model.
//
// sample: function uciTemplate(m, heartbeat, ntp)
func uciTemplate(m modelDef, heartbeat int, ntp bool) string {
	s := "config gateway 'main'\n\toption serial '{{device.sn}}'\n\toption site '{{site.name}}'\n\toption heartbeat '" +
		strconv.Itoa(heartbeat) + "'\n\toption mqtt_host 'mqtt.fleet.example.com'\n\toption mqtt_port '8883'\n"
	if ntp {
		s += "\nconfig timeserver 'ntp'\n\tlist server '{{site.ntp_server}}'\n\tlist server 'pool.ntp.org'\n"
	}
	if hasIface(m, "lora") {
		s += "\nconfig lora 'concentrator'\n\toption region 'AS923'\n\toption server 'lns.example.com'\n\toption port_up '1700'\n\toption port_down '1700'\n"
	}
	if hasIface(m, "lte") {
		s += "\nconfig interface 'wwan'\n\toption proto 'qmi'\n\toption apn '{{site.apn}}'\n\toption pincode '{{secret.sim_pin}}'\n"
	}
	if hasIface(m, "rs485") {
		s += "\nconfig modbus 'rtu'\n\toption device '/dev/ttyS1'\n\toption baudrate '9600'\n\toption parity 'none'\n"
	}
	return s
}

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err) // constant input
	}
	return t
}

// Generate builds the sample fleet. now anchors last-seen and history times.
//
// sample: function initData()
func Generate(now time.Time) Data {
	r := &rng{s: Seed}
	now = now.UTC().Truncate(time.Millisecond)
	ms := func(d float64) time.Duration { return time.Duration(d) * time.Millisecond }

	d := Data{Desired: map[string]int{}}
	siteNames := []string{"Plant A", "Plant B", "Warehouse 3", "Solar Farm North", "Head office", "Cold Store 2"}
	for _, n := range siteNames {
		d.Sites = append(d.Sites, domain.DefaultSite(n))
	}
	for _, m := range models {
		mm := m.Model
		for _, i := range m.ifaces {
			mm.Interfaces = append(mm.Interfaces, domain.Interface{Kind: i.kind, Name: i.name})
		}
		d.Models = append(d.Models, mm)
	}

	// sample: [['1.1.0','stable','2026-05-12',.02],['1.2.0','stable','2026-07-28',.03],['1.3.0-beta.1','beta','2026-09-15',.30]]
	fws := []struct {
		v, ch, date string
		fail        float64
	}{{"1.1.0", "stable", "2026-05-12", .02}, {"1.2.0", "stable", "2026-07-28", .03}, {"1.3.0-beta.1", "beta", "2026-09-15", .30}}
	for _, m := range models {
		p := strings.ToLower(strings.Replace(m.ID, "-", "", 1))
		for _, f := range fws {
			size := 7.4
			if m.ID == "GW-300" {
				size = 11.6
			}
			switch f.v {
			case "1.3.0-beta.1":
				size += 0.5
			case "1.2.0":
				size += 0.2
			}
			d.Firmware = append(d.Firmware, Firmware{
				Firmware: domain.Firmware{Model: m.ID, Version: f.v, Channel: f.ch, SizeMB: toFixed1(size),
					File: p + "-openwrt-23.05.4-" + f.v + "-squashfs-sysupgrade.bin", SHA256: r.hex(64)},
				Released: f.date, FailRate: f.fail,
			})
		}
	}

	d.Packages = []domain.Package{
		{Name: "gw-agent", Version: "0.9.2", Prev: "0.9.1", Channel: "stable", Models: []string{"GW-100", "GW-200", "GW-300"}, Desc: "Management agent"},
		{Name: "lora-pkt-fwd", Version: "4.0.2", Prev: "4.0.1", Channel: "stable", Models: []string{"GW-100", "GW-200"}, Desc: "LoRa packet forwarder"},
		{Name: "modbus-bridge", Version: "1.4.0", Prev: "1.3.2", Channel: "stable", Models: []string{"GW-200"}, Desc: "RS485 Modbus to MQTT"},
		{Name: "lte-watchdog", Version: "1.1.0", Prev: "1.0.4", Channel: "stable", Models: []string{"GW-200", "GW-300"}, Desc: "Restarts the LTE link when it drops"},
		{Name: "ble-scanner", Version: "0.3.1", Prev: "0.3.0", Channel: "beta", Models: []string{"GW-300"}, Desc: "Bluetooth LE beacon scanner"},
	}

	for _, m := range models {
		d.Desired[m.ID] = 2
		d.Configs = append(d.Configs,
			domain.ConfigVersion{Model: m.ID, V: 1, By: "Release engineer", At: date("2026-06-03"), Note: "Initial template", Text: uciTemplate(m, 60, false)},
			domain.ConfigVersion{Model: m.ID, V: 2, By: "Release engineer", At: date("2026-08-19"), Note: "Heartbeat 60 s to 30 s, add NTP servers", Text: uciTemplate(m, 30, true)},
		)
	}

	for mi, m := range models {
		for i := range 20 {
			// Order of r.next() calls must match the sample exactly.
			code := strings.Replace(m.ID, "-", "", 1)
			sn := code + "-" + r.pick([]string{"2431", "2438", "2507", "2519"}) + "-" + leftPad(mi*100+i+1, 5)
			online := r.next() > .08
			temp := r.next() * 26
			if r.next() < .07 {
				temp += 16
			}
			temp = jsRound(44 + temp)
			pk := map[string]string{}
			for _, p := range d.Packages {
				if contains(p.Models, m.ID) {
					if r.next() < .75 {
						pk[p.Name] = p.Version
					} else {
						pk[p.Name] = p.Prev
					}
				}
			}
			dev := Device{Device: domain.Device{SN: sn, Model: m.ID, Online: online, Temp: temp}, Packages: pk}
			dev.HwRev = r.pick([]string{"A1", "A2", "B0"})
			dev.Site = r.pick(siteNames)
			dev.FW = "1.1.0"
			if r.next() < .55 {
				dev.FW = "1.2.0"
			}
			dev.CfgVer = 1
			if r.next() < .82 {
				dev.CfgVer = 2
			}
			dev.CPU = jsRound(6 + r.next()*40)
			dev.RAM = jsRound(34 + r.next()*44)
			ram := float64(m.RAMMB)
			if r.next() < .08 {
				dev.TmpFreeMB = toFixed1(4 + r.next()*2)
			} else {
				dev.TmpFreeMB = toFixed1(ram/4 + r.next()*ram/5)
			}
			dev.UptimeH = jsRound(r.next() * 900)
			if hasIface(m, "lte") {
				rssi := -jsRound(62 + r.next()*44)
				dev.RSSI = &rssi
			}
			for _, fi := range m.ifaces {
				ident := r.ifID(fi.kind)
				up := online && r.next() > .05 // sample short-circuits: no draw when offline
				dev.Interfaces = append(dev.Interfaces, domain.Interface{Kind: fi.kind, Name: fi.name, Ident: ident, Up: up})
			}
			for range 30 {
				dev.Temps = append(dev.Temps, temp+jsRound((r.next()-.5)*6))
			}
			if online {
				dev.LastSeen = now.Add(-ms(jsRound(r.next() * 50e3)))
			} else {
				dev.LastSeen = now.Add(-ms(jsRound((20 + r.next()*300) * 60e3)))
			}
			dev.History = []Event{{At: now.Add(-ms(86400e3 * (30 + jsRound(r.next()*120)))), Msg: "Provisioned with factory certificate"}}
			d.Devices = append(d.Devices, dev)
		}
	}
	return d
}

func leftPad(n, width int) string {
	s := strconv.Itoa(n)
	return strings.Repeat("0", max(0, width-len(s))) + s
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
