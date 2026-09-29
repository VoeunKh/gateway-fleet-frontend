// Package fleet is the read service behind the console API. It combines store
// queries with the domain rules (health, drift, template rendering, diff).
package fleet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

// Page sizes for list endpoints.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// ErrNotFound wraps a missing entity; the message is user-facing.
type ErrNotFound struct{ Msg string }

func (e *ErrNotFound) Error() string { return e.Msg }

// ErrInput is a bad query parameter; the message is user-facing.
type ErrInput struct{ Msg string }

func (e *ErrInput) Error() string { return e.Msg }

// Service serves read models.
type Service struct {
	st  *store.Store
	now func() time.Time
}

// New builds a Service; now may be nil for time.Now.
func New(st *store.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, now: now}
}

// Page is a cursor and page size from the query string.
type Page struct {
	Cursor string
	Limit  int
}

func (p Page) limit() (int, error) {
	switch {
	case p.Limit == 0:
		return DefaultLimit, nil
	case p.Limit < 0 || p.Limit > MaxLimit:
		return 0, &ErrInput{Msg: fmt.Sprintf("limit must be between 1 and %d.", MaxLimit)}
	}
	return p.Limit, nil
}

var badCursor = &ErrInput{Msg: "The cursor is not valid. Start again from the first page."}

func encodeCursor(parts ...string) *string {
	s := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
	return &s
}

func decodeCursor(c string, n int) ([]string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, badCursor
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != n {
		return nil, badCursor
	}
	return parts, nil
}

// timeCursor encodes an (at, id) position used by newest-first lists.
func timeCursor(at time.Time, id string) *string {
	return encodeCursor(strconv.FormatInt(at.UnixMilli(), 10), id)
}

func decodeTimeCursor(c string) (time.Time, string, error) {
	parts, err := decodeCursor(c, 2)
	if err != nil {
		return time.Time{}, "", err
	}
	msec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", badCursor
	}
	return time.UnixMilli(msec).UTC(), parts[1], nil
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func summary(d domain.Device, desired map[string]int) DeviceSummary {
	return DeviceSummary{
		SN: d.SN, Model: d.Model, HwRev: d.HwRev, Site: d.Site, FW: d.FW, CfgVer: d.CfgVer,
		Drift: domain.Drift(d, desired[d.Model]), Health: domain.DeviceHealth(d),
		Online: d.Online, Temp: d.Temp, LastSeen: d.LastSeen,
	}
}

func alertView(a store.AlertRow) AlertView {
	return AlertView{ID: a.ID, SN: a.SN, Site: a.Site, Kind: a.Kind, Severity: a.Severity, Message: a.Message,
		State: a.State, At: a.At, AckedBy: a.AckedBy, ResolvedAt: timePtr(a.ResolvedAt)}
}

func doneCount(r domain.Rollout) int {
	n := 0
	for _, d := range r.Devices {
		if d.State.Terminal() {
			n++
		}
	}
	return n
}

// Overview is the fleet overview screen.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	models, err := s.st.Models(ctx)
	if err != nil {
		return Overview{}, err
	}
	desired, err := s.st.DesiredConfigs(ctx)
	if err != nil {
		return Overview{}, err
	}
	fws, err := s.st.FirmwareList(ctx)
	if err != nil {
		return Overview{}, err
	}
	var ov Overview
	idx := map[string]int{}
	for i, m := range models {
		idx[m.ID] = i
		ov.Models = append(ov.Models, OverviewModel{ID: m.ID, Name: m.Name, Board: []BoardCell{}, Firmware: []VersionCount{}})
	}
	for _, f := range fws {
		if i, ok := idx[f.Model]; ok {
			ov.Models[i].Firmware = append(ov.Models[i].Firmware, VersionCount{Version: f.Version, Count: f.Devices})
		}
	}
	err = s.st.ScanDevices(ctx, store.DeviceQuery{}, func(_ int64, d domain.Device) bool {
		h := domain.DeviceHealth(d)
		ov.Total++
		if d.Online {
			ov.Online++
		}
		if h == domain.Critical || h == domain.Offline {
			ov.Attention++
		}
		if domain.Drift(d, desired[d.Model]) {
			ov.Drifted++
		}
		if i, ok := idx[d.Model]; ok {
			m := &ov.Models[i]
			m.Count++
			m.Board = append(m.Board, BoardCell{SN: d.SN, Health: h, FW: d.FW})
		}
		return true
	})
	if err != nil {
		return Overview{}, err
	}
	active, err := s.st.Rollouts(ctx, store.RolloutQuery{ActiveOnly: true, Limit: MaxLimit})
	if err != nil {
		return Overview{}, err
	}
	ov.Rollouts = []RolloutMini{}
	for _, r := range active {
		ov.Rollouts = append(ov.Rollouts, RolloutMini{ID: r.ID, Model: r.Model, FW: r.FW, State: r.State, Done: doneCount(r), Total: len(r.Devices)})
	}
	// sample: const al = openAlerts().slice(0,5);
	alerts, err := s.st.Alerts(ctx, store.AlertQuery{States: []domain.AlertState{domain.AlertOpen, domain.AlertAcked}, Limit: 5})
	if err != nil {
		return Overview{}, err
	}
	ov.Alerts = []AlertView{}
	for _, a := range alerts {
		ov.Alerts = append(ov.Alerts, alertView(a))
	}
	return ov, nil
}

// DeviceFilter is the devices list query.
type DeviceFilter struct {
	Q      string
	Model  string
	Status string // healthy, warning, critical, offline, drift or ""
}

// Devices lists devices matching f.
//
// sample: vDevices() — search SN, site or any interface identifier; filter by model and
// by health or "drift".
func (s *Service) Devices(ctx context.Context, f DeviceFilter, p Page) (DeviceList, error) {
	limit, err := p.limit()
	if err != nil {
		return DeviceList{}, err
	}
	switch f.Status {
	case "", "drift", string(domain.Healthy), string(domain.Warning), string(domain.Critical), string(domain.Offline):
	default:
		return DeviceList{}, &ErrInput{Msg: "status must be healthy, warning, critical, offline or drift."}
	}
	var after int64
	if p.Cursor != "" {
		parts, err := decodeCursor(p.Cursor, 1)
		if err != nil {
			return DeviceList{}, err
		}
		if after, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
			return DeviceList{}, badCursor
		}
	}
	desired, err := s.st.DesiredConfigs(ctx)
	if err != nil {
		return DeviceList{}, err
	}
	total, err := s.st.CountDevices(ctx)
	if err != nil {
		return DeviceList{}, err
	}
	list := DeviceList{Items: []DeviceSummary{}, Total: total}
	// Health and drift come from domain rules, so filtering happens here while
	// streaming rows; matched counts every hit for the "X of Y shown" line.
	q := store.DeviceQuery{Q: strings.TrimSpace(f.Q), Model: f.Model}
	var lastPos int64
	more := false
	err = s.st.ScanDevices(ctx, q, func(pos int64, d domain.Device) bool {
		sum := summary(d, desired)
		if f.Status == "drift" && !sum.Drift || f.Status != "" && f.Status != "drift" && string(sum.Health) != f.Status {
			return true
		}
		list.Matched++
		switch {
		case pos <= after:
		case len(list.Items) < limit:
			list.Items = append(list.Items, sum)
			lastPos = pos
		default:
			more = true
		}
		return true
	})
	if err != nil {
		return DeviceList{}, err
	}
	if more {
		list.NextCursor = encodeCursor(strconv.FormatInt(lastPos, 10))
	}
	return list, nil
}

func notFoundDevice(sn string) error {
	return &ErrNotFound{Msg: fmt.Sprintf("No gateway with serial number %s.", sn)}
}

func (s *Service) device(ctx context.Context, sn string) (domain.Device, error) {
	d, err := s.st.Device(ctx, sn)
	if errors.Is(err, store.ErrNotFound) {
		return d, notFoundDevice(sn)
	}
	return d, err
}

// Device is the device detail screen.
func (s *Service) Device(ctx context.Context, sn string) (DeviceDetail, error) {
	d, err := s.device(ctx, sn)
	if err != nil {
		return DeviceDetail{}, err
	}
	desired, err := s.st.DesiredConfigs(ctx)
	if err != nil {
		return DeviceDetail{}, err
	}
	m, err := s.st.Model(ctx, d.Model)
	if err != nil {
		return DeviceDetail{}, err
	}
	ifs, err := s.st.DeviceInterfaces(ctx, sn)
	if err != nil {
		return DeviceDetail{}, err
	}
	pkgs, err := s.st.DevicePackages(ctx, sn)
	if err != nil {
		return DeviceDetail{}, err
	}
	out := DeviceDetail{
		DeviceSummary: summary(d, desired),
		ModelInfo:     ModelView{ID: m.ID, Name: m.Name, SoC: m.SoC, Arch: m.Arch, RAMMB: m.RAMMB, FlashMB: m.FlashMB, OS: m.OS},
		Bricked:       d.Bricked, UptimeH: d.UptimeH, CPU: d.CPU, RAM: d.RAM, TmpFreeMB: d.TmpFreeMB, RSSI: d.RSSI,
		Interfaces: []InterfaceView{}, Packages: []PackageInstall{},
	}
	for _, i := range ifs {
		out.Interfaces = append(out.Interfaces, InterfaceView{Kind: i.Kind, Name: i.Name, Ident: i.Ident, Up: i.Up})
	}
	for _, p := range pkgs {
		out.Packages = append(out.Packages, PackageInstall{Name: p.Name, Installed: p.Installed, Latest: p.Latest, UpToDate: p.Installed == p.Latest})
	}

	// sample: render_vars(d, desired.text) — secrets are always masked here.
	want := desired[d.Model]
	out.Config = DeviceConfig{Desired: want, Reported: d.CfgVer, Drift: domain.Drift(d, want)}
	versions, err := s.st.ConfigVersions(ctx, d.Model)
	if err != nil {
		return DeviceDetail{}, err
	}
	site, err := s.st.Site(ctx, d.Site)
	if errors.Is(err, store.ErrNotFound) {
		site = domain.DefaultSite(d.Site)
	} else if err != nil {
		return DeviceDetail{}, err
	}
	for _, v := range versions {
		if v.V == want {
			if out.Config.Rendered, err = domain.RenderTemplate(v.Text, d.SN, site, nil); err != nil {
				return DeviceDetail{}, err
			}
		}
	}

	id, fw, rd, err := s.st.ActiveRolloutFor(ctx, sn)
	switch {
	case err == nil:
		out.Rollout = &DeviceRollout{ID: id, FW: fw, State: rd.State, Pct: rd.Pct}
	case !errors.Is(err, store.ErrNotFound):
		return DeviceDetail{}, err
	}
	return out, nil
}

// Events is a device's history, newest first.
func (s *Service) Events(ctx context.Context, sn string, p Page) (EventList, error) {
	limit, err := p.limit()
	if err != nil {
		return EventList{}, err
	}
	var after *store.Event
	if p.Cursor != "" {
		at, id, err := decodeTimeCursor(p.Cursor)
		if err != nil {
			return EventList{}, err
		}
		after = &store.Event{At: at, ID: id}
	}
	if _, err := s.device(ctx, sn); err != nil {
		return EventList{}, err
	}
	evs, err := s.st.DeviceEvents(ctx, sn, after, limit+1)
	if err != nil {
		return EventList{}, err
	}
	out := EventList{Items: []EventView{}}
	for i, e := range evs {
		if i == limit {
			last := evs[limit-1]
			out.NextCursor = timeCursor(last.At, last.ID)
			break
		}
		out.Items = append(out.Items, EventView{At: e.At, Msg: e.Msg})
	}
	return out, nil
}

// MetricRanges are the accepted ?range= values. Rollups are kept 30 days.
var MetricRanges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
}

// Metrics returns a device's 5-minute rollups for a range (default 24h).
func (s *Service) Metrics(ctx context.Context, sn, rng string) (Metrics, error) {
	if rng == "" {
		rng = "24h"
	}
	d, ok := MetricRanges[rng]
	if !ok {
		return Metrics{}, &ErrInput{Msg: "range must be 1h, 6h, 24h, 7d or 30d."}
	}
	if _, err := s.device(ctx, sn); err != nil {
		return Metrics{}, err
	}
	rows, err := s.st.Metrics5m(ctx, sn, s.now().Add(-d))
	if err != nil {
		return Metrics{}, err
	}
	out := Metrics{Range: rng, Points: []MetricPoint{}}
	for _, r := range rows {
		out.Points = append(out.Points, MetricPoint{At: r.Bucket, TempAvg: r.TempAvg, TempMax: r.TempMax,
			CPUAvg: r.CPUAvg, RAMAvg: r.RAMAvg, TmpMin: r.TmpMin, RSSIAvg: r.RSSIAvg})
	}
	return out, nil
}

// Firmware lists firmware images with device counts.
func (s *Service) Firmware(ctx context.Context) ([]FirmwareView, error) {
	fws, err := s.st.FirmwareList(ctx)
	if err != nil {
		return nil, err
	}
	out := []FirmwareView{}
	for _, f := range fws {
		out = append(out, FirmwareView{Model: f.Model, Version: f.Version, Channel: f.Channel, Released: f.Released,
			SizeMB: f.SizeMB, File: f.File, SHA256: f.SHA256, Blocked: f.Blocked, Devices: f.Devices})
	}
	return out, nil
}

// Packages lists packages with up-to-date and outdated counts.
func (s *Service) Packages(ctx context.Context) ([]PackageView, error) {
	ps, err := s.st.PackageList(ctx)
	if err != nil {
		return nil, err
	}
	out := []PackageView{}
	for _, p := range ps {
		out = append(out, PackageView{Name: p.Name, Version: p.Version, Prev: p.Prev, Channel: p.Channel,
			Models: p.Models, Desc: p.Desc, UpToDate: p.UpToDate, Outdated: p.Outdated})
	}
	return out, nil
}

// Config returns a model's template versions with diffs and sync counts.
func (s *Service) Config(ctx context.Context, model string) (ConfigView, error) {
	if _, err := s.st.Model(ctx, model); errors.Is(err, store.ErrNotFound) {
		return ConfigView{}, &ErrNotFound{Msg: fmt.Sprintf("No model %s.", model)}
	} else if err != nil {
		return ConfigView{}, err
	}
	desired, err := s.st.DesiredConfigs(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	versions, err := s.st.ConfigVersions(ctx, model)
	if err != nil {
		return ConfigView{}, err
	}
	inSync, drifted, err := s.st.ConfigSync(ctx, model)
	if err != nil {
		return ConfigView{}, err
	}
	out := ConfigView{Model: model, Desired: desired[model], InSync: inSync, Drifted: drifted, Versions: []ConfigVersionView{}}
	for i, v := range versions {
		cv := ConfigVersionView{V: v.V, By: v.By, At: v.At, Note: v.Note, Text: v.Text}
		// sample: vConfig() — "v2 compared with v1", diffLines(prev.text, sel.text)
		if i > 0 {
			for _, l := range domain.DiffLines(versions[i-1].Text, v.Text) {
				cv.Diff = append(cv.Diff, DiffLineView{Op: string(l.Op), Text: l.Text})
			}
		}
		out.Versions = append(out.Versions, cv)
	}
	return out, nil
}

// AlertStates maps ?state= to the states it selects. "active" (default) is open
// plus acknowledged, the sample's "Open" list.
var AlertStates = map[string][]domain.AlertState{
	"":         {domain.AlertOpen, domain.AlertAcked},
	"active":   {domain.AlertOpen, domain.AlertAcked},
	"open":     {domain.AlertOpen},
	"ack":      {domain.AlertAcked},
	"resolved": {domain.AlertResolved},
	"all":      nil,
}

// Alerts lists alerts newest first.
func (s *Service) Alerts(ctx context.Context, state string, p Page) (AlertList, error) {
	states, ok := AlertStates[state]
	if !ok {
		return AlertList{}, &ErrInput{Msg: "state must be active, open, ack, resolved or all."}
	}
	limit, err := p.limit()
	if err != nil {
		return AlertList{}, err
	}
	q := store.AlertQuery{States: states, Limit: limit + 1}
	if p.Cursor != "" {
		at, id, err := decodeTimeCursor(p.Cursor)
		if err != nil {
			return AlertList{}, err
		}
		q.After = &domain.Alert{At: at, ID: id}
	}
	rows, err := s.st.Alerts(ctx, q)
	if err != nil {
		return AlertList{}, err
	}
	out := AlertList{Items: []AlertView{}}
	for i, a := range rows {
		if i == limit {
			last := rows[limit-1]
			out.NextCursor = timeCursor(last.At, last.ID)
			break
		}
		out.Items = append(out.Items, alertView(a))
	}
	return out, nil
}

// Rollouts lists rollouts newest first with their devices.
func (s *Service) Rollouts(ctx context.Context, p Page) (RolloutList, error) {
	limit, err := p.limit()
	if err != nil {
		return RolloutList{}, err
	}
	q := store.RolloutQuery{Limit: limit + 1}
	if p.Cursor != "" {
		parts, err := decodeCursor(p.Cursor, 1)
		if err != nil {
			return RolloutList{}, err
		}
		q.AfterID = parts[0]
	}
	rs, err := s.st.Rollouts(ctx, q)
	if err != nil {
		return RolloutList{}, err
	}
	out := RolloutList{Items: []RolloutView{}}
	for i, r := range rs {
		if i == limit {
			out.NextCursor = encodeCursor(rs[limit-1].ID)
			break
		}
		out.Items = append(out.Items, rolloutView(r))
	}
	return out, nil
}

func rolloutView(r domain.Rollout) RolloutView {
	v := RolloutView{ID: r.ID, Model: r.Model, FW: r.FW, Threshold: r.Threshold, Wave: r.Wave, State: r.State,
		Reason: r.Reason, SoakUntil: timePtr(r.SoakUntil), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		FinishedAt: timePtr(r.FinishedAt), Waves: []int{}, Devices: []RolloutDeviceView{}}
	prev := 0
	for _, c := range r.Counts {
		v.Waves = append(v.Waves, c-prev)
		prev = c
	}
	wave := 0
	for i, d := range r.Devices {
		for wave < len(r.Counts)-1 && i >= r.Counts[wave] {
			wave++
		}
		v.Devices = append(v.Devices, RolloutDeviceView{SN: d.SN, Wave: wave, State: d.State, Pct: d.Pct, From: d.From, Note: d.Note})
		// sample: vRollout() — inflight = !TERMINAL && st!=='queued'
		switch d.State {
		case domain.StSuccess:
			v.Summary.Updated++
		case domain.StRolledBack:
			v.Summary.RolledBack++
		case domain.StBricked:
			v.Summary.Bricked++
		case domain.StSkipped, domain.StDeferred:
			v.Summary.Skipped++
		case domain.StQueued:
			v.Summary.Waiting++
		default:
			v.Summary.InProgress++
		}
	}
	return v
}
