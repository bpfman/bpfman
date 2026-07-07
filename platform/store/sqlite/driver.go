package sqlite

import (
	"errors"
	"strings"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// driverName is the database/sql driver registered by
// github.com/mattn/go-sqlite3, the only SQLite driver this package
// uses. The store links the SQLite C library through cgo, which the
// project requires anyway: the bpfman-ns transport uses a C
// constructor, so CGO_ENABLED=1 is mandatory.
const driverName = "sqlite3"

// isBusyError reports whether err carries a SQLite SQLITE_BUSY
// condition (any primary or extended code that shares the
// SQLITE_BUSY primary). mattn's Error.Code is the primary code,
// so a direct comparison covers SQLITE_BUSY_RECOVERY and
// SQLITE_BUSY_SNAPSHOT too. The retry layer in RunInTransaction
// treats all of these as transient.
func isBusyError(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code == sqlite3.ErrBusy
}

// dsn builds a mattn/go-sqlite3 DSN from a path and pragma key-value
// pairs. Each pair is formatted as _key=value in the query string.
func dsn(path string, pragmas [][2]string) string {
	var s strings.Builder
	s.WriteString(path)
	for i, p := range pragmas {
		if i == 0 {
			s.WriteString("?")
		} else {
			s.WriteString("&")
		}
		s.WriteString("_" + p[0] + "=" + p[1])
	}
	return s.String()
}
