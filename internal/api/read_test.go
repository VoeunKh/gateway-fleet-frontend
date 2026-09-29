package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/fleet"
)

func getJSON[T any](t *testing.T, e *env, c *client, path string) T {
	t.Helper()
	rec := e.do(http.MethodGet, path, "", c, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return v
}

func expectError(t *testing.T, e *env, c *client, path string, status int, code string) string {
	t.Helper()
	rec := e.do(http.MethodGet, path, "", c, nil)
	got, msg := errCode(t, rec)
	if rec.Code != status || got != code {
		t.Errorf("GET %s: %d %s, want %d %s", path, rec.Code, rec.Body, status, code)
	}
	return msg
}

const lteSN = "GW200-2438-00101" // seeded LTE gateway (see seed_test.go)

// Every read endpoint: 401 without a session, 200 for every role (reads have no 403).
func TestReadRoutesAuth(t *testing.T) {
	e := newSeededEnv(t)
	s := &Server{}
	routes := s.readRoutes()
	if len(routes) != 10 {
		t.Fatalf("expected 10 read routes, got %d", len(routes))
	}
	clients := map[string]*client{"admin": e.login("admin"), "release": e.login("release"), "viewer": e.login("viewer")}
	for _, rr := range routes {
		path := strings.NewReplacer("{sn}", lteSN, "{model}", "GW-200").Replace(rr.Pattern)
		if rec := e.do(http.MethodGet, path, "", nil, nil); rec.Code != 401 {
			t.Errorf("anonymous %s: %d", path, rec.Code)
		}
		for role, c := range clients {
			if rec := e.do(http.MethodGet, path, "", c, nil); rec.Code != 200 {
				t.Errorf("%s %s: %d %s", role, path, rec.Code, rec.Body)
			}
		}
	}
}

func TestOverview(t *testing.T) {
	e := newSeededEnv(t)
	ov := getJSON[fleet.Overview](t, e, e.login("viewer"), "/api/overview")
	// 9 offline + 2 online at >= 80 °C; see seed_test.go
	if ov.Total != 60 || ov.Online != 51 || ov.Drifted != 12 || ov.Attention != 11 {
		t.Errorf("counts: %+v", ov)
	}
	if len(ov.Models) != 3 || ov.Models[1].ID != "GW-200" || ov.Models[1].Name != "Outdoor LTE" {
		t.Fatalf("models: %+v", ov.Models)
	}
	for _, m := range ov.Models {
		sum := 0
		for _, f := range m.Firmware {
			sum += f.Count
		}
		if m.Count != 20 || len(m.Board) != 20 || sum != 20 || len(m.Firmware) != 3 {
			t.Errorf("%s: count=%d board=%d fw=%+v", m.ID, m.Count, len(m.Board), m.Firmware)
		}
	}
	if ov.Models[0].Board[0].SN != "GW100-2431-00001" {
		t.Errorf("board keeps sample order: %s", ov.Models[0].Board[0].SN)
	}
	if len(ov.Alerts) != 5 || len(ov.Rollouts) != 0 {
		t.Errorf("alerts=%d rollouts=%d", len(ov.Alerts), len(ov.Rollouts))
	}
}

func TestDevicesListFilters(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	tests := []struct {
		query   string
		matched int
		first   string
	}{
		{"", 60, "GW100-2431-00001"},
		{"model=GW-300", 20, ""},
		{"status=offline", 9, "GW100-2431-00006"},
		{"status=drift", 12, ""},
		{"status=critical", 2, ""},
		{"q=" + url.QueryEscape("8928394032859425682"), 1, lteSN},  // ICCID
		{"q=" + url.QueryEscape("imei 080764246623510"), 1, lteSN}, // IMEI, case-insensitive
		{"q=" + url.QueryEscape("GW200-2438-00101"), 1, lteSN},     // serial
		{"q=" + url.QueryEscape("70:b3:d5"), 60, ""},               // MAC prefix; every model has eth
		{"q=" + url.QueryEscape("  plant a  "), -1, ""},            // site, trimmed, case-insensitive
		{"q=%25", 0, ""},                                           // LIKE wildcard is escaped
		{"q=zzz&model=GW-100", 0, ""},
	}
	for _, tc := range tests {
		l := getJSON[fleet.DeviceList](t, e, c, "/api/devices?"+tc.query)
		if l.Total != 60 || (tc.matched >= 0 && l.Matched != tc.matched) || len(l.Items) != l.Matched || l.NextCursor != nil {
			t.Errorf("%q: matched=%d items=%d total=%d next=%v", tc.query, l.Matched, len(l.Items), l.Total, l.NextCursor)
		}
		if tc.first != "" && (len(l.Items) == 0 || l.Items[0].SN != tc.first) {
			t.Errorf("%q: first item %+v", tc.query, l.Items)
		}
		if tc.matched == -1 {
			for _, d := range l.Items {
				if d.Site != "Plant A" {
					t.Errorf("site search returned %s", d.Site)
				}
			}
			if l.Matched == 0 {
				t.Error("site search found nothing")
			}
		}
	}
	for _, q := range []string{"status=broken", "limit=0x", "limit=501", "limit=-1", "cursor=%21%21"} {
		expectError(t, e, c, "/api/devices?"+q, 400, "bad_request")
	}
}

func TestDevicesPagination(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	all := getJSON[fleet.DeviceList](t, e, c, "/api/devices")
	var got []string
	path := "/api/devices?limit=25"
	for pages := 0; ; pages++ {
		l := getJSON[fleet.DeviceList](t, e, c, path)
		if l.Matched != 60 {
			t.Fatalf("matched %d", l.Matched)
		}
		for _, d := range l.Items {
			got = append(got, d.SN)
		}
		if l.NextCursor == nil {
			if pages != 2 {
				t.Errorf("pages=%d, want 3", pages+1)
			}
			break
		}
		path = "/api/devices?limit=25&cursor=" + *l.NextCursor
	}
	if len(got) != 60 {
		t.Fatalf("paged %d devices", len(got))
	}
	for i, d := range all.Items {
		if got[i] != d.SN {
			t.Fatalf("page order differs at %d", i)
		}
	}
	// A page that ends exactly on the last match has no next cursor.
	if l := getJSON[fleet.DeviceList](t, e, c, "/api/devices?status=offline&limit=9"); l.NextCursor != nil || len(l.Items) != 9 {
		t.Errorf("exact page: next=%v items=%d", l.NextCursor, len(l.Items))
	}
	l := getJSON[fleet.DeviceList](t, e, c, "/api/devices?status=offline&limit=5")
	l2 := getJSON[fleet.DeviceList](t, e, c, "/api/devices?status=offline&limit=5&cursor="+*l.NextCursor)
	if len(l2.Items) != 4 || l2.NextCursor != nil || l2.Items[0].SN == l.Items[4].SN {
		t.Errorf("filtered second page: %+v", l2)
	}
}

func TestDeviceDetail(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	d := getJSON[fleet.DeviceDetail](t, e, c, "/api/devices/"+lteSN)
	if d.SN != lteSN || d.ModelInfo.Name != "Outdoor LTE" || d.ModelInfo.Arch != "mipsel_24kc" || d.RSSI == nil {
		t.Errorf("detail: %+v", d)
	}
	if len(d.Interfaces) != 5 || d.Interfaces[1].Kind != "lte" || !strings.HasPrefix(d.Interfaces[1].Ident, "IMEI 080764246623510") {
		t.Errorf("interfaces: %+v", d.Interfaces)
	}
	if len(d.Packages) != 4 || d.Packages[0].Name != "gw-agent" {
		t.Errorf("packages: %+v", d.Packages)
	}
	cfg := d.Config.Rendered
	if d.Config.Desired != 2 || !strings.Contains(cfg, "option serial '"+lteSN+"'") ||
		!strings.Contains(cfg, "option pincode '"+domain.SecretMask+"'") || strings.Contains(cfg, "{{") {
		t.Errorf("config: %+v", d.Config)
	}
	if d.Rollout != nil {
		t.Errorf("no rollout expected: %+v", d.Rollout)
	}
	gw100 := getJSON[fleet.DeviceDetail](t, e, c, "/api/devices/GW100-2431-00001")
	if gw100.RSSI != nil || strings.Contains(gw100.Config.Rendered, "pincode") {
		t.Errorf("GW-100 has no LTE: %+v", gw100)
	}
	for _, p := range []string{"/api/devices/NOPE", "/api/devices/NOPE/events", "/api/devices/NOPE/metrics"} {
		if msg := expectError(t, e, c, p, 404, "not_found"); msg != "No gateway with serial number NOPE." {
			t.Errorf("%s: %q", p, msg)
		}
	}
}

func TestEventsAndMetrics(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	ev := getJSON[fleet.EventList](t, e, c, "/api/devices/"+lteSN+"/events")
	if len(ev.Items) != 1 || ev.Items[0].Msg != "Provisioned with factory certificate" || ev.NextCursor != nil {
		t.Errorf("events: %+v", ev)
	}
	expectError(t, e, c, "/api/devices/"+lteSN+"/events?cursor=x", 400, "bad_request")

	m := getJSON[fleet.Metrics](t, e, c, "/api/devices/"+lteSN+"/metrics")
	if m.Range != "24h" || len(m.Points) != 30 || m.Points[0].TempAvg == nil || m.Points[0].CPUAvg != nil {
		t.Errorf("metrics: range=%s points=%d", m.Range, len(m.Points))
	}
	if !m.Points[29].At.Equal(e.clock.now()) || !m.Points[0].At.Before(m.Points[1].At) {
		t.Errorf("metrics order: %v … %v", m.Points[0].At, m.Points[29].At)
	}
	// seeded buckets end at the clock time (12:00); 1h covers 11:00–12:00
	if h := getJSON[fleet.Metrics](t, e, c, "/api/devices/"+lteSN+"/metrics?range=1h"); len(h.Points) != 13 {
		t.Errorf("1h points: %d", len(h.Points))
	}
	expectError(t, e, c, "/api/devices/"+lteSN+"/metrics?range=2y", 400, "bad_request")
}

func TestFirmwarePackagesConfig(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	fw := getJSON[struct{ Items []fleet.FirmwareView }](t, e, c, "/api/firmware").Items
	total := 0
	for _, f := range fw {
		total += f.Devices
	}
	if len(fw) != 9 || total != 60 || fw[2].Version != "1.3.0-beta.1" || fw[2].Devices != 0 || fw[2].Channel != "beta" || len(fw[0].SHA256) != 64 {
		t.Errorf("firmware: %+v", fw)
	}
	pk := getJSON[struct{ Items []fleet.PackageView }](t, e, c, "/api/packages").Items
	if len(pk) != 5 || pk[0].Name != "gw-agent" || pk[0].UpToDate+pk[0].Outdated != 60 ||
		pk[2].Name != "modbus-bridge" || pk[2].UpToDate+pk[2].Outdated != 20 {
		t.Errorf("packages: %+v", pk)
	}
	cfg := getJSON[fleet.ConfigView](t, e, c, "/api/config/GW-200")
	if cfg.Desired != 2 || cfg.InSync+cfg.Drifted != 20 || len(cfg.Versions) != 2 || cfg.Versions[0].Diff != nil {
		t.Fatalf("config: %+v", cfg)
	}
	var added []string
	for _, l := range cfg.Versions[1].Diff {
		if l.Op == "+" {
			added = append(added, strings.TrimSpace(l.Text))
		}
	}
	if !strings.Contains(strings.Join(added, "\n"), "list server 'pool.ntp.org'") {
		t.Errorf("v2 diff adds: %v", added)
	}
	if msg := expectError(t, e, c, "/api/config/GW-999", 404, "not_found"); msg != "No model GW-999." {
		t.Errorf("msg %q", msg)
	}
}

func TestAlertsList(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	active := getJSON[fleet.AlertList](t, e, c, "/api/alerts")
	if len(active.Items) != 11 || active.NextCursor != nil {
		t.Fatalf("active: %d", len(active.Items))
	}
	a := active.Items[0]
	if a.Site == "" || a.State != domain.AlertOpen || a.ResolvedAt != nil {
		t.Errorf("alert %+v", a)
	}
	if l := getJSON[fleet.AlertList](t, e, c, "/api/alerts?state=resolved"); len(l.Items) != 0 {
		t.Errorf("resolved: %d", len(l.Items))
	}
	seen := map[string]bool{}
	path := "/api/alerts?limit=4"
	for {
		l := getJSON[fleet.AlertList](t, e, c, path)
		for _, x := range l.Items {
			if seen[x.ID] {
				t.Fatalf("duplicate alert %s", x.ID)
			}
			seen[x.ID] = true
		}
		if l.NextCursor == nil {
			break
		}
		path = "/api/alerts?limit=4&cursor=" + *l.NextCursor
	}
	if len(seen) != 11 {
		t.Errorf("paged %d alerts", len(seen))
	}
	expectError(t, e, c, "/api/alerts?state=closed", 400, "bad_request")
}

func TestRolloutsList(t *testing.T) {
	e := newSeededEnv(t)
	c := e.login("viewer")
	if l := getJSON[fleet.RolloutList](t, e, c, "/api/rollouts"); len(l.Items) != 0 {
		t.Fatalf("rollouts: %d", len(l.Items))
	}
	ctx := context.Background()
	devs := map[string]domain.Device{}
	var all []domain.Device
	for _, it := range getJSON[fleet.DeviceList](t, e, c, "/api/devices?model=GW-200").Items {
		d := domain.Device{SN: it.SN, Model: it.Model, FW: it.FW, Online: it.Online, Temp: it.Temp, TmpFreeMB: 30}
		devs[it.SN] = d
		all = append(all, d)
	}
	plan, err := domain.PlanWaves(domain.PlanInput{Model: "GW-200", TargetFW: "1.3.0-beta.1",
		Firmware: &domain.Firmware{}, Waves: "1,10,50,100", Devices: all})
	if err != nil {
		t.Fatal(err)
	}
	r := domain.NewRollout("R-001", "GW-200", "1.3.0-beta.1", 10, plan, devs, "Admin", e.clock.now())
	r.Devices[0].State, r.Devices[1].State = domain.StSuccess, domain.StDownloading
	r.Devices[1].Pct = 40
	if err := e.store.CreateRollout(ctx, r); err != nil {
		t.Fatal(err)
	}
	l := getJSON[fleet.RolloutList](t, e, c, "/api/rollouts")
	if len(l.Items) != 1 {
		t.Fatalf("rollouts: %+v", l)
	}
	v := l.Items[0]
	if v.ID != "R-001" || v.State != domain.RolloutRunning || v.Waves[0] != 1 || len(v.Devices) != 20 ||
		v.Devices[0].Wave != 0 || v.Devices[1].Wave != 1 || v.Devices[19].Wave != len(v.Waves)-1 {
		t.Errorf("rollout: %+v", v)
	}
	if v.Summary.Updated != 1 || v.Summary.InProgress != 1 || v.Summary.Waiting != 18 || v.FinishedAt != nil {
		t.Errorf("summary: %+v", v.Summary)
	}
	ov := getJSON[fleet.Overview](t, e, c, "/api/overview")
	if len(ov.Rollouts) != 1 || ov.Rollouts[0].Done != 1 || ov.Rollouts[0].Total != 20 {
		t.Errorf("overview rollouts: %+v", ov.Rollouts)
	}
	d := getJSON[fleet.DeviceDetail](t, e, c, "/api/devices/"+r.Devices[1].SN)
	if d.Rollout == nil || d.Rollout.ID != "R-001" || d.Rollout.State != domain.StDownloading || d.Rollout.Pct != 40 {
		t.Errorf("device rollout: %+v", d.Rollout)
	}
	if d := getJSON[fleet.DeviceDetail](t, e, c, "/api/devices/"+r.Devices[0].SN); d.Rollout != nil {
		t.Errorf("finished device should not show rollout: %+v", d.Rollout)
	}
	expectError(t, e, c, "/api/rollouts?cursor=***", 400, "bad_request")
}

func TestJSONIsGzipped(t *testing.T) {
	e := newSeededEnv(t)
	rec := e.do(http.MethodGet, "/api/overview", "", e.login("viewer"), map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("overview: %d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
}
