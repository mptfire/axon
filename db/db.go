package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
)

const (
	paramMaxOpenConns    = "pool_max_conns"
	paramMaxIdleConns    = "pool_max_idle_conns"
	paramConnMaxLifetime = "pool_conn_max_lifetime"
	paramConnMaxIdleTime = "pool_conn_max_idle_time"

	defaultMaxOpenConns = 10
)

// OpenPostgres opens a PostgreSQL database connection pool from a DSN string. It supports custom
// query parameters for pool configuration: pool_max_conns (default 10), pool_max_idle_conns,
// pool_conn_max_lifetime, and pool_conn_max_idle_time. These parameters are stripped from
// the DSN before passing it to the driver.
func OpenPostgres(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	q := u.Query()
	maxOpenConns, err := extractIntParam(q, paramMaxOpenConns, defaultMaxOpenConns)
	if err != nil {
		return nil, err
	}
	maxIdleConns, err := extractIntParam(q, paramMaxIdleConns, 0)
	if err != nil {
		return nil, err
	}
	connMaxLifetime, err := extractDurationParam(q, paramConnMaxLifetime, 0)
	if err != nil {
		return nil, err
	}
	connMaxIdleTime, err := extractDurationParam(q, paramConnMaxIdleTime, 0)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpenConns)
	if maxIdleConns > 0 {
		db.SetMaxIdleConns(maxIdleConns)
	}
	if connMaxLifetime > 0 {
		db.SetConnMaxLifetime(connMaxLifetime)
	}
	if connMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(connMaxIdleTime)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	return db, nil
}

func extractIntParam(q url.Values, key string, defaultValue int) (int, error) {
	s := q.Get(key)
	if s == "" {
		return defaultValue, nil
	}
	q.Del(key)
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", key, s, err)
	}
	return v, nil
}

func extractDurationParam(q url.Values, key string, defaultValue time.Duration) (time.Duration, error) {
	s := q.Get(key)
	if s == "" {
		return defaultValue, nil
	}
	q.Del(key)
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", key, s, err)
	}
	return d, nil
}

// ExecTx executes a function within a database transaction. If the function returns an error,
// the transaction is rolled back. Otherwise, the transaction is committed.
func ExecTx(db *sql.DB, f func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// QueryTx executes a function within a database transaction and returns the result. If the function
// returns an error, the transaction is rolled back. Otherwise, the transaction is committed.
func QueryTx[T any](db *sql.DB, f func(tx *sql.Tx) (T, error)) (T, error) {
	tx, err := db.Begin()
	if err != nil {
		var zero T
		return zero, err
	}
	defer tx.Rollback()
	t, err := f(tx)
	if err != nil {
		return t, err
	}
	if err := tx.Commit(); err != nil {
		return t, err
	}
	return t, nil
}
