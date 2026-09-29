package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
)

func ms(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func msOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// likeEscape makes q safe inside LIKE '%…%' ESCAPE '\'.
func likeEscape(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// Models returns all models in catalogue order.
func (s *Store) Models(ctx context.Context) ([]domain.Model, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT id,name,soc,arch,ram_mb,flash_mb,os FROM models ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("models: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Model
	for rows.Next() {
		var m domain.Model
		if err := rows.Scan(&m.ID, &m.Name, &m.SoC, &m.Arch, &m.RAMMB, &m.FlashMB, &m.OS); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DesiredConfigs maps model id to its desired config version.
func (s *Store) DesiredConfigs(ctx context.Context) (map[string]int, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT model, desired FROM model_config`)
	if err != nil {
		return nil, fmt.Errorf("desired configs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var m string
		var v int
		if err := rows.Scan(&m, &v); err != nil {
			return nil, fmt.Errorf("scan desired: %w", err)
		}
		out[m] = v
	}
	return out, rows.Err()
}

const deviceCols = `d.rowid, d.sn, d.model, d.hw_rev, d.site, d.fw, d.cfg_ver, d.online, d.bricked, d.last_seen,
	d.temp, d.cpu, d.ram, d.tmp_free_mb, d.rssi, d.uptime_h`

func scanDevice(sc interface{ Scan(...any) error }) (int64, domain.Device, error) {
	var d domain.Device
	var rowid, lastSeen int64
	var rssi sql.NullFloat64
	err := sc.Scan(&rowid, &d.SN, &d.Model, &d.HwRev, &d.Site, &d.FW, &d.CfgVer, &d.Online, &d.Bricked, &lastSeen,
		&d.Temp, &d.CPU, &d.RAM, &d.TmpFreeMB, &rssi, &d.UptimeH)
	if err != nil {
		return 0, d, fmt.Errorf("scan device: %w", err)
	}
	d.LastSeen = ms(lastSeen)
	if rssi.Valid {
		d.RSSI = &rssi.Float64
	}
	return rowid, d, nil
}

// DeviceQuery filters ScanDevices. Empty fields match everything.
type DeviceQuery struct {
	Q     string // substring of SN, site, or an interface identifier (MAC, IMEI, ICCID)
	Model string
}

// ScanDevices streams devices matching q in stable (insertion) order. fn gets
// the row's cursor position; returning false stops the scan.
func (s *Store) ScanDevices(ctx context.Context, q DeviceQuery, fn func(pos int64, d domain.Device) bool) error {
	where, args := []string{"1=1"}, []any{}
	if q.Model != "" {
		where = append(where, "d.model = ?")
		args = append(args, q.Model)
	}
	if q.Q != "" {
		p := likeEscape(q.Q)
		where = append(where, `(d.sn LIKE ? ESCAPE '\' OR d.site LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM device_interfaces i WHERE i.sn = d.sn AND i.ident LIKE ? ESCAPE '\'))`)
		args = append(args, p, p, p)
	}
	// #nosec G202 -- where holds only constant fragments; values are bound
	rows, err := s.read.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE `+strings.Join(where, " AND ")+` ORDER BY d.rowid`, args...)
	if err != nil {
		return fmt.Errorf("scan devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		pos, d, err := scanDevice(rows)
		if err != nil {
			return err
		}
		if !fn(pos, d) {
			break
		}
	}
	return rows.Err()
}

// Device returns one device by serial number.
func (s *Store) Device(ctx context.Context, sn string) (domain.Device, error) {
	_, d, err := scanDevice(s.read.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.sn = ?`, sn))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// Model returns one model.
func (s *Store) Model(ctx context.Context, id string) (domain.Model, error) {
	var m domain.Model
	err := s.read.QueryRowContext(ctx, `SELECT id,name,soc,arch,ram_mb,flash_mb,os FROM models WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.SoC, &m.Arch, &m.RAMMB, &m.FlashMB, &m.OS)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, fmt.Errorf("model: %w", err)
	}
	return m, nil
}

// Site returns a site's template values.
func (s *Store) Site(ctx context.Context, name string) (domain.Site, error) {
	st := domain.Site{Name: name}
	err := s.read.QueryRowContext(ctx, `SELECT ntp_server, apn FROM sites WHERE name = ?`, name).Scan(&st.NTPServer, &st.APN)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, fmt.Errorf("site: %w", err)
	}
	return st, nil
}

// DeviceInterfaces lists a device's interfaces in model order.
func (s *Store) DeviceInterfaces(ctx context.Context, sn string) ([]domain.Interface, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT kind, name, ident, up FROM device_interfaces WHERE sn = ? ORDER BY rowid`, sn)
	if err != nil {
		return nil, fmt.Errorf("interfaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Interface
	for rows.Next() {
		var i domain.Interface
		if err := rows.Scan(&i.Kind, &i.Name, &i.Ident, &i.Up); err != nil {
			return nil, fmt.Errorf("scan interface: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// InstalledPackage is a package on a device next to the latest version.
type InstalledPackage struct {
	Name      string
	Installed string
	Latest    string
}

// DevicePackages lists a device's packages in catalogue order.
func (s *Store) DevicePackages(ctx context.Context, sn string) ([]InstalledPackage, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT dp.name, dp.version, p.version FROM device_packages dp JOIN packages p ON p.name = dp.name
		WHERE dp.sn = ? ORDER BY p.rowid`, sn)
	if err != nil {
		return nil, fmt.Errorf("device packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []InstalledPackage
	for rows.Next() {
		var p InstalledPackage
		if err := rows.Scan(&p.Name, &p.Installed, &p.Latest); err != nil {
			return nil, fmt.Errorf("scan device package: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Event is a device history entry.
type Event struct {
	ID  string
	At  time.Time
	Msg string
}

// DeviceEvents returns a device's history newest first, starting after the
// (at, id) cursor when given.
func (s *Store) DeviceEvents(ctx context.Context, sn string, after *Event, limit int) ([]Event, error) {
	q, args := `SELECT id, at, msg FROM device_events WHERE sn = ?`, []any{sn}
	if after != nil {
		q += ` AND (at < ? OR (at = ? AND id < ?))`
		args = append(args, after.At.UnixMilli(), after.At.UnixMilli(), after.ID)
	}
	rows, err := s.read.QueryContext(ctx, q+` ORDER BY at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("device events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var e Event
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.Msg); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.At = ms(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Rollup is one 5-minute metrics bucket. Nil means no samples.
type Rollup struct {
	Bucket                                   time.Time
	TempAvg, TempMax, CPUAvg, RAMAvg, TmpMin *float64
	RSSIAvg                                  *float64
}

func nullable(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}

// Metrics5m returns a device's rollups since the given time, oldest first.
func (s *Store) Metrics5m(ctx context.Context, sn string, since time.Time) ([]Rollup, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT bucket, temp_avg, temp_max, cpu_avg, ram_avg, tmp_min, rssi_avg
		FROM metrics_5m WHERE sn = ? AND bucket >= ? ORDER BY bucket`, sn, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Rollup
	for rows.Next() {
		var b int64
		var ta, tm, c, r, tmin, rs sql.NullFloat64
		if err := rows.Scan(&b, &ta, &tm, &c, &r, &tmin, &rs); err != nil {
			return nil, fmt.Errorf("scan metrics: %w", err)
		}
		out = append(out, Rollup{Bucket: ms(b), TempAvg: nullable(ta), TempMax: nullable(tm), CPUAvg: nullable(c),
			RAMAvg: nullable(r), TmpMin: nullable(tmin), RSSIAvg: nullable(rs)})
	}
	return out, rows.Err()
}

// FirmwareImage is a firmware row with how many devices run it.
type FirmwareImage struct {
	domain.Firmware
	Released string
	Devices  int
}

// FirmwareList returns all images in catalogue order with device counts.
func (s *Store) FirmwareList(ctx context.Context) ([]FirmwareImage, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT f.model, f.version, f.channel, f.released, f.size_mb, f.file, f.sha256, f.blocked,
			(SELECT count(*) FROM devices d WHERE d.model = f.model AND d.fw = f.version)
		FROM firmware f ORDER BY f.rowid`)
	if err != nil {
		return nil, fmt.Errorf("firmware: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []FirmwareImage
	for rows.Next() {
		var f FirmwareImage
		if err := rows.Scan(&f.Model, &f.Version, &f.Channel, &f.Released, &f.SizeMB, &f.File, &f.SHA256, &f.Blocked, &f.Devices); err != nil {
			return nil, fmt.Errorf("scan firmware: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PackageStatus is a package with install counts.
type PackageStatus struct {
	domain.Package
	UpToDate int
	Outdated int
}

// PackageList returns all packages with up-to-date and outdated counts.
func (s *Store) PackageList(ctx context.Context) ([]PackageStatus, error) {
	rows, err := s.read.QueryContext(ctx, `
		SELECT p.name, p.version, p.prev, p.channel, p.models, p.descr,
			count(dp.sn) FILTER (WHERE dp.version = p.version),
			count(dp.sn) FILTER (WHERE dp.version <> p.version)
		FROM packages p LEFT JOIN device_packages dp ON dp.name = p.name
		GROUP BY p.name ORDER BY p.rowid`)
	if err != nil {
		return nil, fmt.Errorf("packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PackageStatus
	for rows.Next() {
		var p PackageStatus
		var models string
		if err := rows.Scan(&p.Name, &p.Version, &p.Prev, &p.Channel, &models, &p.Desc, &p.UpToDate, &p.Outdated); err != nil {
			return nil, fmt.Errorf("scan package: %w", err)
		}
		p.Models = strings.Split(models, ",")
		out = append(out, p)
	}
	return out, rows.Err()
}

// ConfigVersions returns a model's template versions, oldest first.
func (s *Store) ConfigVersions(ctx context.Context, model string) ([]domain.ConfigVersion, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT model, v, by, at, note, text FROM config_versions WHERE model = ? ORDER BY v`, model)
	if err != nil {
		return nil, fmt.Errorf("config versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ConfigVersion
	for rows.Next() {
		var c domain.ConfigVersion
		var at int64
		if err := rows.Scan(&c.Model, &c.V, &c.By, &at, &c.Note, &c.Text); err != nil {
			return nil, fmt.Errorf("scan config version: %w", err)
		}
		c.At = ms(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConfigSync counts a model's devices in sync with and drifted from the desired version.
func (s *Store) ConfigSync(ctx context.Context, model string) (inSync, drifted int, err error) {
	err = s.read.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE d.cfg_ver = mc.desired), count(*) FILTER (WHERE d.cfg_ver <> mc.desired)
		FROM devices d JOIN model_config mc ON mc.model = d.model WHERE d.model = ?`, model).Scan(&inSync, &drifted)
	if err != nil {
		return 0, 0, fmt.Errorf("config sync: %w", err)
	}
	return inSync, drifted, nil
}

// AlertRow is an alert with the device's site.
type AlertRow struct {
	domain.Alert
	Site string
}

// AlertQuery selects alerts. States empty means all.
type AlertQuery struct {
	States []domain.AlertState
	After  *domain.Alert // cursor: continue after this (At, ID)
	Limit  int
}

// Alerts returns alerts newest first.
func (s *Store) Alerts(ctx context.Context, q AlertQuery) ([]AlertRow, error) {
	where, args := []string{"1=1"}, []any{}
	if len(q.States) > 0 {
		where = append(where, "a.state IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.States)), ",")+")")
		for _, st := range q.States {
			args = append(args, st)
		}
	}
	if q.After != nil {
		where = append(where, "(a.at < ? OR (a.at = ? AND a.id < ?))")
		args = append(args, q.After.At.UnixMilli(), q.After.At.UnixMilli(), q.After.ID)
	}
	// #nosec G202 -- where holds only constant fragments and placeholders
	rows, err := s.read.QueryContext(ctx, `
		SELECT a.id, a.sn, a.kind, a.severity, a.message, a.state, a.at, a.acked_by, a.resolved_at, COALESCE(d.site, '')
		FROM alerts a LEFT JOIN devices d ON d.sn = a.sn
		WHERE `+strings.Join(where, " AND ")+` ORDER BY a.at DESC, a.id DESC LIMIT ?`, append(args, q.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AlertRow
	for rows.Next() {
		var a AlertRow
		var at, resolved int64
		if err := rows.Scan(&a.ID, &a.SN, &a.Kind, &a.Severity, &a.Message, &a.State, &at, &a.AckedBy, &resolved, &a.Site); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		a.At, a.ResolvedAt = ms(at), ms(resolved)
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateRollout stores a new rollout and its devices, recording each device's wave.
func (s *Store) CreateRollout(ctx context.Context, r domain.Rollout) error {
	counts, err := json.Marshal(r.Counts)
	if err != nil {
		return fmt.Errorf("encode counts: %w", err)
	}
	return s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO rollouts(id,model,fw,threshold,counts,wave,state,reason,override,soak_until,created_by,created_at,finished_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.Model, r.FW, r.Threshold, string(counts), r.Wave, r.State, r.Reason, r.Override,
			msOrZero(r.SoakUntil), r.CreatedBy, msOrZero(r.CreatedAt), msOrZero(r.FinishedAt)); err != nil {
			return fmt.Errorf("insert rollout: %w", err)
		}
		wave := 0
		for i, d := range r.Devices {
			for wave < len(r.Counts)-1 && i >= r.Counts[wave] {
				wave++
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO rollout_devices(rollout_id,seq,wave,sn,state,pct,from_fw,note,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
				r.ID, i, wave, d.SN, d.State, d.Pct, d.From, d.Note, msOrZero(r.CreatedAt)); err != nil {
				return fmt.Errorf("insert rollout device: %w", err)
			}
		}
		return nil
	})
}

// RolloutQuery selects rollouts.
type RolloutQuery struct {
	ActiveOnly bool
	AfterID    string // cursor: rollouts created before this one
	Limit      int
}

// Rollouts returns rollouts newest first, each with its devices in rollout order.
func (s *Store) Rollouts(ctx context.Context, q RolloutQuery) ([]domain.Rollout, error) {
	where, args := []string{"1=1"}, []any{}
	if q.ActiveOnly {
		where = append(where, "state IN ('running','soaking','paused')")
	}
	if q.AfterID != "" {
		where = append(where, "rowid < (SELECT rowid FROM rollouts WHERE id = ?)")
		args = append(args, q.AfterID)
	}
	// #nosec G202 -- constant fragments only
	rows, err := s.read.QueryContext(ctx, `SELECT id,model,fw,threshold,counts,wave,state,reason,override,soak_until,created_by,created_at,finished_at
		FROM rollouts WHERE `+strings.Join(where, " AND ")+` ORDER BY rowid DESC LIMIT ?`, append(args, q.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("rollouts: %w", err)
	}
	var out []domain.Rollout
	for rows.Next() {
		var r domain.Rollout
		var counts string
		var soak, created, finished int64
		if err := rows.Scan(&r.ID, &r.Model, &r.FW, &r.Threshold, &counts, &r.Wave, &r.State, &r.Reason, &r.Override,
			&soak, &r.CreatedBy, &created, &finished); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan rollout: %w", err)
		}
		if err := json.Unmarshal([]byte(counts), &r.Counts); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode counts of %s: %w", r.ID, err)
		}
		r.SoakUntil, r.CreatedAt, r.FinishedAt = ms(soak), ms(created), ms(finished)
		out = append(out, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("rollouts: %w", err)
	}
	for i := range out {
		if out[i].Devices, err = s.rolloutDevices(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) rolloutDevices(ctx context.Context, id string) ([]domain.RolloutDevice, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT sn, state, pct, from_fw, note FROM rollout_devices WHERE rollout_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, fmt.Errorf("rollout devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.RolloutDevice
	for rows.Next() {
		var d domain.RolloutDevice
		if err := rows.Scan(&d.SN, &d.State, &d.Pct, &d.From, &d.Note); err != nil {
			return nil, fmt.Errorf("scan rollout device: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ActiveRolloutFor returns the active rollout in which sn is not yet terminal.
func (s *Store) ActiveRolloutFor(ctx context.Context, sn string) (id, fw string, dev domain.RolloutDevice, err error) {
	err = s.read.QueryRowContext(ctx, `
		SELECT r.id, r.fw, rd.sn, rd.state, rd.pct, rd.from_fw, rd.note
		FROM rollout_devices rd JOIN rollouts r ON r.id = rd.rollout_id
		WHERE rd.sn = ? AND r.state IN ('running','soaking','paused')
			AND rd.state NOT IN ('success','rolledback','bricked','skipped','deferred')
		ORDER BY r.rowid DESC LIMIT 1`, sn).Scan(&id, &fw, &dev.SN, &dev.State, &dev.Pct, &dev.From, &dev.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", dev, ErrNotFound
	}
	if err != nil {
		return "", "", dev, fmt.Errorf("active rollout for %s: %w", sn, err)
	}
	return id, fw, dev, nil
}
