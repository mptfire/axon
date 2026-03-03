# pgimport

One-off migration script to import ntfy data from SQLite to PostgreSQL.

This is **not** a generic migration tool. It only works with specific SQLite schema versions
(message cache v14, user db v6, web push v1) and their corresponding PostgreSQL schemas.
If your database versions differ, this tool will refuse to run.

## Build

```bash
go build -o pgimport ./tools/pgimport/
```

## Usage

```bash
# Using CLI flags
pgimport \
  --database-url "postgres://user:pass@host:5432/ntfy?sslmode=require" \
  --cache-file /var/cache/ntfy/cache.db \
  --auth-file /var/lib/ntfy/user.db \
  --web-push-file /var/lib/ntfy/webpush.db

# Using server.yml (flags override config values)
pgimport --config /etc/ntfy/server.yml
```

## Prerequisites

- PostgreSQL schema must already be set up (run ntfy with `database-url` once)
- ntfy must not be running during the import
- All three SQLite files are optional; only the ones specified will be imported

## Notes

- The tool is idempotent and safe to re-run
- After importing, row counts and content are verified against the SQLite sources
- Invalid UTF-8 in messages is replaced with the Unicode replacement character
