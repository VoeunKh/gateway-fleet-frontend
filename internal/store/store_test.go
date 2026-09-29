package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/seed"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMigrateIdempotentAndPragmas(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	n, err := s.Migrate(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second migrate: n=%d err=%v", n, err)
	}
	var mode string
	var timeout, fk int
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode=%q err=%v", mode, err)
	}
	if err := s.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
		t.Errorf("busy_timeout=%d err=%v", timeout, err)
	}
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys=%d err=%v", fk, err)
	}
	if _, err := s.DB().Exec(`INSERT INTO sites VALUES('x','y','z')`); err == nil {
		t.Error("read pool must be query-only")
	}
}

func TestIndexesExist(t *testing.T) {
	s := openTemp(t)
	for _, idx := range []string{"devices_model", "devices_site", "device_interfaces_ident", "alerts_state_sn", "rollout_devices_wave"} {
		var n int
		if err := s.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n); err != nil || n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}
}

func TestSeed(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.UnixMilli(1790000000000).UTC()
	if err := s.Seed(ctx, seed.Generate(now), now); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{
		"devices": 60, "models": 3, "sites": 6, "firmware": 9, "packages": 5, "config_versions": 6, "model_config": 3,
		"device_events": 60, "metrics_5m": 60 * 30,
		// 9 offline for >10 min + 2 online at >= 80 °C (checkAlerts() on first load)
		"alerts": 11,
	}
	for table, want := range counts {
		var n int
		if err := s.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s: %d rows (err %v), want %d", table, n, err, want)
		}
	}
	var rssi sql.NullFloat64
	if err := s.DB().QueryRow(`SELECT rssi FROM devices WHERE sn='GW100-2431-00001'`).Scan(&rssi); err != nil || rssi.Valid {
		t.Errorf("GW-100 has no LTE, rssi=%v err=%v", rssi, err)
	}
	var sn string
	if err := s.DB().QueryRow(`SELECT sn FROM device_interfaces WHERE ident LIKE '%ICCID 8928394032859425682%'`).Scan(&sn); err != nil || sn != "GW200-2438-00101" {
		t.Errorf("iccid lookup: %s %v", sn, err)
	}
	if err := s.Seed(ctx, seed.Generate(now), now); !errors.Is(err, ErrAlreadySeeded) {
		t.Errorf("second seed: %v", err)
	}
}

func TestWriteSerialisesAndRollsBack(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO sites VALUES(?, 'n', 'a')`, string(rune('A'+i)))
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	boom := errors.New("boom")
	err := s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO sites VALUES('rollback-me','n','a')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	var n int
	_ = s.DB().QueryRow(`SELECT count(*) FROM sites`).Scan(&n)
	if n != 50 {
		t.Errorf("sites=%d, want 50", n)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Write(cctx, func(*sql.Tx) error { return nil }); err == nil {
		t.Error("cancelled context should fail")
	}
}

func TestWriteAfterClose(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.Write(context.Background(), func(*sql.Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Errorf("err=%v", err)
	}
}

func TestCountDevices(t *testing.T) {
	s := openTemp(t)
	if n, err := s.CountDevices(context.Background()); err != nil || n != 0 {
		t.Errorf("n=%d err=%v", n, err)
	}
}

func TestUsersSessionsAudit(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.UnixMilli(1790000000000).UTC()
	id, err := s.CreateUser(ctx, "alice", "hash", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "alice", "hash", "admin", now); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := s.CreateUser(ctx, "bob", "hash", "root", now); err == nil {
		t.Error("invalid role accepted by CHECK constraint")
	}
	u, err := s.UserByUsername(ctx, "alice")
	if err != nil || u.ID != id || u.Role != "admin" || !u.CreatedAt.Equal(now) {
		t.Fatalf("user %+v %v", u, err)
	}
	if _, err := s.UserByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user: %v", err)
	}
	if err := s.CreateSession(ctx, "h1", "c1", id, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "h2", "c2", id, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ss, err := s.SessionByTokenHash(ctx, "h1", now)
	if err != nil || ss.CSRF != "c1" || ss.User.Username != "alice" {
		t.Fatalf("session %+v %v", ss, err)
	}
	if _, err := s.SessionByTokenHash(ctx, "h2", now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: %v", err)
	}
	if n, err := s.DeleteExpiredSessions(ctx, now.Add(2*time.Minute)); err != nil || n != 1 {
		t.Errorf("expired deleted %d %v", n, err)
	}
	if err := s.DeleteSession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByTokenHash(ctx, "h1", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: %v", err)
	}
	if err := s.Audit(ctx, AuditEntry{At: now.Add(time.Second), Action: "system.test"}); err != nil {
		t.Fatal(err)
	}
	log, err := s.AuditLog(ctx, 10)
	if err != nil || len(log) != 4 || log[0].Action != "system.test" || log[0].UserID != "" || log[3].Action != "user.create" {
		t.Errorf("audit %+v %v", log, err)
	}
}
