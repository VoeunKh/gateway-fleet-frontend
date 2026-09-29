// Package store is the SQLite access layer. Reads use a small pool; all writes go
// through one writer goroutine (docs/adr/0002-sqlite-first.md).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no cgo)
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrClosed is returned by Write after Close.
var ErrClosed = errors.New("store closed")

// Store holds the read pool and the single writer.
type Store struct {
	read  *sql.DB
	write *sql.DB
	jobs  chan job
	done  chan struct{}
}

type job struct {
	ctx context.Context
	fn  func(*sql.Tx) error
	res chan error
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens (creating if needed) the database at path. Call Migrate before use.
func Open(path string) (*Store, error) {
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	w.SetMaxOpenConns(1)
	// Ping creates the file and switches it to WAL before readers connect.
	if err := w.Ping(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	r.SetMaxOpenConns(4)

	s := &Store{read: r, write: w, jobs: make(chan job), done: make(chan struct{})}
	go s.writer()
	return s, nil
}

func (s *Store) writer() {
	for {
		select {
		case j := <-s.jobs:
			j.res <- s.runTx(j.ctx, j.fn)
		case <-s.done:
			return
		}
	}
}

func (s *Store) runTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Write runs fn in a transaction on the single writer goroutine.
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	res := make(chan error, 1)
	select {
	case s.jobs <- job{ctx: ctx, fn: fn, res: res}:
	case <-s.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-res
}

// DB returns the read-only pool.
func (s *Store) DB() *sql.DB { return s.read }

// Close stops the writer and closes both pools.
func (s *Store) Close() error {
	close(s.done)
	return errors.Join(s.read.Close(), s.write.Close())
}

// Migrate applies all pending migrations and returns how many ran.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("migrations fs: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, s.write, sub)
	if err != nil {
		return 0, fmt.Errorf("goose provider: %w", err)
	}
	res, err := p.Up(ctx)
	if err != nil {
		return len(res), fmt.Errorf("migrate: %w", err)
	}
	return len(res), nil
}

// CountDevices returns the number of devices.
func (s *Store) CountDevices(ctx context.Context) (int, error) {
	var n int
	if err := s.read.QueryRowContext(ctx, `SELECT count(*) FROM devices`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count devices: %w", err)
	}
	return n, nil
}
