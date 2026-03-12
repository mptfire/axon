package db

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	replicaHealthCheckInterval = 30 * time.Second
	replicaHealthCheckTimeout  = 2 * time.Second
)

// Beginner is an interface for types that can begin a database transaction.
// Both *sql.DB and *DB implement this.
type Beginner interface {
	Begin() (*sql.Tx, error)
}

// DB wraps a primary *sql.DB and optional read replicas. All standard query/exec methods
// delegate to the primary. The ReadOnly() method returns a *sql.DB from a healthy replica
// (round-robin), falling back to the primary if no replicas are configured or all are unhealthy.
type DB struct {
	primary  *sql.DB
	replicas []*replica
	counter  atomic.Uint64
	cancel   context.CancelFunc
}

type replica struct {
	db      *sql.DB
	healthy atomic.Bool
}

// NewDB creates a new DB that wraps the given primary and optional replica connections.
// If replicas is nil or empty, ReadOnly() simply returns the primary.
// Replicas start unhealthy and are checked immediately by a background goroutine.
func NewDB(primary *sql.DB, replicas []*sql.DB) *DB {
	ctx, cancel := context.WithCancel(context.Background())
	d := &DB{
		primary:  primary,
		replicas: make([]*replica, len(replicas)),
		cancel:   cancel,
	}
	for i, r := range replicas {
		d.replicas[i] = &replica{db: r} // healthy defaults to false
	}
	if len(d.replicas) > 0 {
		go d.healthCheckLoop(ctx)
	}
	return d
}

// Primary returns the underlying primary *sql.DB. This is only intended for
// one-time schema setup during store initialization, not for regular queries.
func (d *DB) Primary() *sql.DB {
	return d.primary
}

// Query delegates to the primary database.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.primary.Query(query, args...)
}

// QueryRow delegates to the primary database.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.primary.QueryRow(query, args...)
}

// Exec delegates to the primary database.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.primary.Exec(query, args...)
}

// Begin delegates to the primary database.
func (d *DB) Begin() (*sql.Tx, error) {
	return d.primary.Begin()
}

// Ping delegates to the primary database.
func (d *DB) Ping() error {
	return d.primary.Ping()
}

// Close closes the primary database and all replicas, and stops the health-check goroutine.
func (d *DB) Close() error {
	d.cancel()
	for _, r := range d.replicas {
		r.db.Close()
	}
	return d.primary.Close()
}

// ReadOnly returns a *sql.DB suitable for read-only queries. It round-robins across healthy
// replicas. If all replicas are unhealthy or none are configured, the primary is returned.
func (d *DB) ReadOnly() *sql.DB {
	if len(d.replicas) == 0 {
		return d.primary
	}
	n := len(d.replicas)
	start := int(d.counter.Add(1) - 1)
	for i := 0; i < n; i++ {
		r := d.replicas[(start+i)%n]
		if r.healthy.Load() {
			return r.db
		}
	}
	return d.primary
}

// healthCheckLoop checks replicas immediately, then periodically on a ticker.
func (d *DB) healthCheckLoop(ctx context.Context) {
	d.checkReplicas(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(replicaHealthCheckInterval):
			d.checkReplicas(ctx)
		}
	}
}

// checkReplicas pings each replica with a timeout and updates its health status.
func (d *DB) checkReplicas(ctx context.Context) {
	for _, r := range d.replicas {
		wasHealthy := r.healthy.Load()
		pingCtx, cancel := context.WithTimeout(ctx, replicaHealthCheckTimeout)
		err := r.db.PingContext(pingCtx)
		cancel()
		if err != nil {
			r.healthy.Store(false)
			if wasHealthy {
				log.Error("Database replica is now unhealthy: %s", err)
			}
		} else {
			r.healthy.Store(true)
			if !wasHealthy {
				log.Info("Database replica is now healthy again")
			}
		}
	}
}
