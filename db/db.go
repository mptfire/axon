package db

import (
	"database/sql"
	"sync/atomic"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	replicaHealthCheckInterval = 5 * time.Second
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
}

type replica struct {
	db          *sql.DB
	healthy     atomic.Bool
	lastChecked atomic.Int64
}

// NewDB creates a new DB that wraps the given primary and optional replica connections.
// If replicas is nil or empty, ReadOnly() simply returns the primary.
func NewDB(primary *sql.DB, replicas []*sql.DB) *DB {
	d := &DB{
		primary:  primary,
		replicas: make([]*replica, len(replicas)),
	}
	for i, r := range replicas {
		rep := &replica{db: r}
		rep.healthy.Store(true)
		d.replicas[i] = rep
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

// Close closes the primary database and all replicas.
func (d *DB) Close() error {
	for _, r := range d.replicas {
		r.db.Close()
	}
	return d.primary.Close()
}

// ReadOnly returns a *sql.DB suitable for read-only queries. It round-robins across healthy
// replicas. If a replica's health status is stale (older than replicaHealthCheckInterval), it
// is re-checked with a ping. If all replicas are unhealthy or none are configured, the primary
// is returned.
func (d *DB) ReadOnly() *sql.DB {
	if len(d.replicas) == 0 {
		return d.primary
	}
	n := len(d.replicas)
	start := int(d.counter.Add(1) - 1)
	for i := 0; i < n; i++ {
		r := d.replicas[(start+i)%n]
		if d.isHealthy(r) {
			return r.db
		}
	}
	return d.primary
}

// isHealthy returns whether the replica is healthy. If the cached health status is stale,
// it pings the replica and updates the cache.
func (d *DB) isHealthy(r *replica) bool {
	now := time.Now().Unix()
	lastChecked := r.lastChecked.Load()
	if now-lastChecked >= int64(replicaHealthCheckInterval.Seconds()) {
		if r.lastChecked.CompareAndSwap(lastChecked, now) {
			wasHealthy := r.healthy.Load()
			if err := r.db.Ping(); err != nil {
				r.healthy.Store(false)
				if wasHealthy {
					log.Error("Database replica is now unhealthy: %s", err)
				}
				return false
			}
			r.healthy.Store(true)
			if !wasHealthy {
				log.Info("Database replica is now healthy again")
			}
			return true
		}
	}
	return r.healthy.Load()
}
