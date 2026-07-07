package sqlite

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

// migrationsFS holds the ordered SQL migrations applied to the store.
// Each file is named NNNNN_description.sql and carries goose Up/Down
// annotations; the numeric prefix is the schema version the file
// advances the database to. The lowest is 00001_baseline.sql, the
// initial schema. Add a new file with the next number to evolve the
// schema.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// errSchemaVersionMismatch reports that the on-disk schema version is
// not the version this build expects. The database is never deleted to
// resolve a mismatch: doing so would orphan the BPF programs, pins, and
// links the database is the only record of. A mismatch is surfaced to
// the caller, not silently repaired by wiping live state.
var errSchemaVersionMismatch = errors.New("schema version mismatch")

// migrationProvider builds a goose provider over the embedded
// migrations bound to the store's database handle. goose is silenced so
// migration activity is reported through the store's own slog records
// rather than goose's stdlib logger. Constructing a provider performs
// no database I/O; it only parses the embedded files.
func (s *sqliteStore) migrationProvider() (*goose.Provider, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("locate embedded migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, sub, goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}

// migrate brings the database up to the latest schema version by
// applying every pending migration in order, each in its own
// transaction. A database already at the latest version is left
// untouched. A database newer than this build understands is refused,
// not downgraded and not wiped.
func (s *sqliteStore) migrate(ctx context.Context) error {
	provider, err := s.migrationProvider()
	if err != nil {
		return err
	}

	target := latestMigrationVersion(provider)

	// GetDBVersion creates the goose version table on a fresh
	// database and reports 0; it returns the recorded version on an
	// existing one. Reading it before Up lets us refuse a
	// from-the-future database rather than no-op past it.
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if current > target {
		return fmt.Errorf("%w: database is at version %d, newer than this build supports (%d); refusing to open", errSchemaVersionMismatch, current, target)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if len(results) > 0 {
		s.logger.InfoContext(ctx, "applied schema migrations", "count", len(results), "from", current, "to", target)
	}

	return nil
}

// checkSchemaVersion verifies, without writing, that the database is at
// the schema version this build expects. Read-oriented callers open the
// store without the writer lock, so this path must not create the goose
// version table or apply migrations: it only reads the recorded version
// and compares it against the latest embedded migration.
func (s *sqliteStore) checkSchemaVersion(ctx context.Context) error {
	provider, err := s.migrationProvider()
	if err != nil {
		return err
	}

	target := latestMigrationVersion(provider)

	versionStore, err := database.NewStore(goose.DialectSQLite3, goose.DefaultTablename)
	if err != nil {
		return fmt.Errorf("create version store: %w", err)
	}

	current, err := versionStore.GetLatestVersion(ctx, s.db)
	if err != nil {
		return fmt.Errorf("%w: cannot read schema version (database not initialised?): %w", errSchemaVersionMismatch, err)
	}

	if current != target {
		return fmt.Errorf("%w: have %d, want %d", errSchemaVersionMismatch, current, target)
	}
	return nil
}

// latestMigrationVersion returns the highest version among the embedded
// migrations, i.e. the schema version this build targets.
func latestMigrationVersion(provider *goose.Provider) int64 {
	var latest int64
	for _, src := range provider.ListSources() {
		if src.Version > latest {
			latest = src.Version
		}
	}
	return latest
}
