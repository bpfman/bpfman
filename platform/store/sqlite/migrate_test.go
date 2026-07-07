package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/lock"
)

// addNotesMigration is a synthetic second migration used to prove that
// a real schema change applied on top of the baseline preserves data.
const addNotesMigration = `-- +goose Up
ALTER TABLE map_sets ADD COLUMN notes TEXT;

-- +goose Down
ALTER TABLE map_sets DROP COLUMN notes;
`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// openStore opens (creating and migrating) a file store at dbPath under
// the writer lock and closes it, returning any error from New.
func openStore(ctx context.Context, dbPath, lockPath string) error {
	return lock.Run(ctx, lockPath, func(ctx context.Context, wl lock.WriterScope) error {
		store, err := New(ctx, dbPath, discardLogger(), wl)
		if err != nil {
			return err
		}
		return store.Close()
	})
}

// openRaw opens a bare connection to dbPath for test probes and
// closes it on cleanup.
func openRaw(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// latestKnownVersion is the highest version among the embedded
// migrations, i.e. the version a freshly opened store lands at.
func latestKnownVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	sub, err := fs.Sub(migrationsFS, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, sub, goose.WithLogger(goose.NopLogger()))
	require.NoError(t, err)
	return latestMigrationVersion(provider)
}

// recordedVersion reads the version goose has recorded for the
// database, without writing.
func recordedVersion(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()
	store, err := database.NewStore(goose.DialectSQLite3, goose.DefaultTablename)
	require.NoError(t, err)
	v, err := store.GetLatestVersion(ctx, db)
	require.NoError(t, err)
	return v
}

func tableExists(t *testing.T, ctx context.Context, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n))
	return n == 1
}

func countRows(t *testing.T, ctx context.Context, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n))
	return n
}

// TestMigrateFreshDatabaseLandsAtLatest proves a brand-new database is
// migrated up to the latest schema version and carries the schema.
func TestMigrateFreshDatabaseLandsAtLatest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "store.db")
	lockPath := filepath.Join(dir, ".lock")

	require.NoError(t, openStore(ctx, dbPath, lockPath))

	db := openRaw(t, dbPath)
	require.Equal(t, latestKnownVersion(t, db), recordedVersion(t, ctx, db))
	require.True(t, tableExists(t, ctx, db, "managed_programs"), "schema should be present")
}

// TestMigrateAdoptsExistingDatabaseWithoutWipe is the headline of this
// change: reopening a database that already carries the schema but has
// no goose bookkeeping (the shape every pre-goose database has on disk)
// adopts it in place. The data survives -- it is no longer deleted on a
// version change -- and goose takes over version tracking.
func TestMigrateAdoptsExistingDatabaseWithoutWipe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "store.db")
	lockPath := filepath.Join(dir, ".lock")

	// Create a baseline-schema database and write a row. A pre-goose
	// database carries the baseline shape only -- later migrations did
	// not exist when it was written -- so seed with UpTo(1), not a full
	// open, which would bake in every later migration and make adoption
	// re-apply them onto a schema that already has their changes.
	seed := openRaw(t, dbPath)
	sub, err := fs.Sub(migrationsFS, "migrations")
	require.NoError(t, err)
	seedProvider, err := goose.NewProvider(goose.DialectSQLite3, seed, sub, goose.WithLogger(goose.NopLogger()))
	require.NoError(t, err)
	_, err = seedProvider.UpTo(ctx, 1)
	require.NoError(t, err)
	_, err = seed.ExecContext(ctx, "INSERT INTO map_sets(id, pin_path, created_at) VALUES (1, '/pin', 'now')")
	require.NoError(t, err)

	// Strip goose's bookkeeping and stamp the legacy PRAGMA
	// user_version to mimic a database written by the pre-goose build.
	_, err = seed.ExecContext(ctx, "DROP TABLE goose_db_version")
	require.NoError(t, err)
	_, err = seed.ExecContext(ctx, "PRAGMA user_version = 16")
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	// Reopening must adopt the existing schema rather than wipe it,
	// then migrate it forward to the latest version.
	require.NoError(t, openStore(ctx, dbPath, lockPath))

	db := openRaw(t, dbPath)
	require.Equal(t, 1, countRows(t, ctx, db, "map_sets"), "row must survive reopen")
	require.Equal(t, latestKnownVersion(t, db), recordedVersion(t, ctx, db))
}

// TestMigrateRefusesNewerDatabaseWithoutWipe proves a database recorded
// at a version newer than this build understands is refused, not
// downgraded and not deleted.
func TestMigrateRefusesNewerDatabaseWithoutWipe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "store.db")
	lockPath := filepath.Join(dir, ".lock")

	require.NoError(t, openStore(ctx, dbPath, lockPath))
	seed := openRaw(t, dbPath)
	_, err := seed.ExecContext(ctx, "INSERT INTO map_sets(id, pin_path, created_at) VALUES (1, '/pin', 'now')")
	require.NoError(t, err)

	// Record a version far beyond any embedded migration.
	_, err = seed.ExecContext(ctx, "INSERT INTO goose_db_version(version_id, is_applied) VALUES (9999, 1)")
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	err = openStore(ctx, dbPath, lockPath)
	require.Error(t, err)
	require.ErrorIs(t, err, errSchemaVersionMismatch)

	db := openRaw(t, dbPath)
	require.Equal(t, 1, countRows(t, ctx, db, "map_sets"), "data must be left intact on refusal")
}

// TestForwardMigrationPreservesData proves the framework supports real
// future migrations: a schema change applied on top of the baseline
// advances the version and leaves existing rows in place.
func TestForwardMigrationPreservesData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	// Assemble a migrations directory: the real baseline plus a
	// synthetic follow-up that adds a column.
	migDir := filepath.Join(dir, "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	baseline, err := migrationsFS.ReadFile("migrations/00001_baseline.sql")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "00001_baseline.sql"), baseline, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "00002_add_notes.sql"), []byte(addNotesMigration), 0o644))

	dbPath := filepath.Join(dir, "store.db")
	db, err := sql.Open(driverName, dbPath)
	require.NoError(t, err)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS(migDir), goose.WithLogger(goose.NopLogger()))
	require.NoError(t, err)

	// Apply only the baseline, then write a row.
	_, err = provider.UpTo(ctx, 1)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO map_sets(id, pin_path, created_at) VALUES (1, '/pin', 'now')")
	require.NoError(t, err)

	// Apply the follow-up migration.
	_, err = provider.Up(ctx)
	require.NoError(t, err)

	version, err := provider.GetDBVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	require.Equal(t, 1, countRows(t, ctx, db, "map_sets"), "row must survive the migration")

	// The new column must exist and be writable.
	_, err = db.ExecContext(ctx, "UPDATE map_sets SET notes = 'hello' WHERE id = 1")
	require.NoError(t, err)
}
