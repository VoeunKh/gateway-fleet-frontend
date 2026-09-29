package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/seed"
)

// ErrAlreadySeeded is returned by Seed when the database already has devices.
var ErrAlreadySeeded = errors.New("database already has devices")

const bucket5m = 5 * time.Minute

// Seed loads generated sample data in one transaction, including the alerts the
// sample opens on first load (checkAlerts() after initData()).
func (s *Store) Seed(ctx context.Context, d seed.Data, now time.Time) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM devices`).Scan(&n); err != nil {
			return fmt.Errorf("count devices: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("seed: %w (%d)", ErrAlreadySeeded, n)
		}
		ins := inserter{ctx: ctx, tx: tx}
		for _, m := range d.Models {
			ins.exec(`INSERT INTO models(id,name,soc,arch,ram_mb,flash_mb,os) VALUES(?,?,?,?,?,?,?)`,
				m.ID, m.Name, m.SoC, m.Arch, m.RAMMB, m.FlashMB, m.OS)
		}
		for _, st := range d.Sites {
			ins.exec(`INSERT INTO sites(name,ntp_server,apn) VALUES(?,?,?)`, st.Name, st.NTPServer, st.APN)
		}
		for _, f := range d.Firmware {
			ins.exec(`INSERT INTO firmware(model,version,channel,released,size_mb,file,sha256,blocked) VALUES(?,?,?,?,?,?,?,?)`,
				f.Model, f.Version, f.Channel, f.Released, f.SizeMB, f.File, f.SHA256, f.Blocked)
		}
		for _, p := range d.Packages {
			ins.exec(`INSERT INTO packages(name,version,prev,channel,models,descr) VALUES(?,?,?,?,?,?)`,
				p.Name, p.Version, p.Prev, p.Channel, strings.Join(p.Models, ","), p.Desc)
		}
		for _, c := range d.Configs {
			ins.exec(`INSERT INTO config_versions(model,v,by,at,note,text) VALUES(?,?,?,?,?,?)`,
				c.Model, c.V, c.By, c.At.UnixMilli(), c.Note, c.Text)
		}
		for _, m := range d.Models {
			ins.exec(`INSERT INTO model_config(model,desired) VALUES(?,?)`, m.ID, d.Desired[m.ID])
		}
		lastBucket := now.Truncate(bucket5m)
		for _, dev := range d.Devices {
			ins.device(dev, lastBucket)
			for _, c := range domain.EvaluateAlerts(dev.Device, now, nil).Open {
				ins.exec(`INSERT INTO alerts(id,sn,kind,severity,message,state,at) VALUES(?,?,?,?,?,?,?)`,
					ulid.Make().String(), dev.SN, c.Kind, c.Severity, c.Message, domain.AlertOpen, now.UnixMilli())
			}
		}
		return ins.err
	})
}

// inserter keeps the first error so the seed code reads as a flat list.
type inserter struct {
	ctx context.Context
	tx  *sql.Tx
	err error
}

func (i *inserter) exec(q string, args ...any) {
	if i.err != nil {
		return
	}
	if _, err := i.tx.ExecContext(i.ctx, q, args...); err != nil {
		i.err = fmt.Errorf("seed %s: %w", strings.Fields(q)[2], err)
	}
}

func (i *inserter) device(d seed.Device, lastBucket time.Time) {
	i.exec(`INSERT INTO devices(sn,model,hw_rev,site,fw,cfg_ver,online,bricked,last_seen,temp,cpu,ram,tmp_free_mb,rssi,uptime_h)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.SN, d.Model, d.HwRev, d.Site, d.FW, d.CfgVer, d.Online, d.Bricked, d.LastSeen.UnixMilli(),
		d.Temp, d.CPU, d.RAM, d.TmpFreeMB, d.RSSI, d.UptimeH)
	for _, f := range d.Interfaces {
		i.exec(`INSERT INTO device_interfaces(sn,kind,name,ident,up) VALUES(?,?,?,?,?)`, d.SN, f.Kind, f.Name, f.Ident, f.Up)
	}
	for name, v := range d.Packages {
		i.exec(`INSERT INTO device_packages(sn,name,version) VALUES(?,?,?)`, d.SN, name, v)
	}
	for _, e := range d.History {
		i.exec(`INSERT INTO device_events(id,sn,at,msg) VALUES(?,?,?,?)`, ulid.Make().String(), d.SN, e.At.UnixMilli(), e.Msg)
	}
	// The sample's 30-point sparkline becomes the last 30 five-minute rollups.
	for k, t := range d.Temps {
		b := lastBucket.Add(-time.Duration(len(d.Temps)-1-k) * bucket5m)
		i.exec(`INSERT INTO metrics_5m(sn,bucket,temp_avg,temp_max) VALUES(?,?,?,?)`, d.SN, b.UnixMilli(), t, t)
	}
}
