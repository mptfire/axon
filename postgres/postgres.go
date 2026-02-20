package postgres

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
)

const defaultMaxOpenConns = 25

// OpenDB opens a PostgreSQL database connection pool from a DSN string. It supports custom
// query parameters for pool configuration: pool_max_conns (default 25), pool_max_idle_conns,
// pool_conn_max_lifetime, and pool_conn_max_idle_time. These parameters are stripped from
// the DSN before passing it to the driver.
func OpenDB(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	q := u.Query()
	maxOpenConns, err := extractIntParam(q, "pool_max_conns", defaultMaxOpenConns)
	if err != nil {
		return nil, err
	}
	maxIdleConns, err := extractIntParam(q, "pool_max_idle_conns", 0)
	if err != nil {
		return nil, err
	}
	connMaxLifetime, err := extractDurationParam(q, "pool_conn_max_lifetime", 0)
	if err != nil {
		return nil, err
	}
	connMaxIdleTime, err := extractDurationParam(q, "pool_conn_max_idle_time", 0)
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
		return nil, err
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
