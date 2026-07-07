package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/lock"
	"github.com/bpfman/bpfman/platform"
)

// assertAllStatementsPrepared reflects over every *sql.Stmt field on the
// store and fails, naming the field, if any is nil. It is drift-proof: a
// statement field added to the struct is checked automatically, with no
// edit to this test.
func assertAllStatementsPrepared(t *testing.T, s *sqliteStore, where string) {
	t.Helper()

	v := reflect.ValueOf(s).Elem()
	typ := v.Type()
	stmtPtr := reflect.TypeFor[*sql.Stmt]()

	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type != stmtPtr {
			continue
		}
		checked++
		if v.Field(i).IsNil() {
			t.Errorf("%s: prepared-statement field %s is nil", where, typ.Field(i).Name)
		}
	}

	if checked == 0 {
		t.Fatalf("found no *sql.Stmt fields on sqliteStore; the reflection assumption is broken")
	}
	t.Logf("%s: %d prepared-statement fields all non-nil", where, checked)
}

// TestPreparedStatementsPreparedAndReboundInTx guards the parallel lists
// that back the store's prepared statements: the struct fields, their
// preparation at New, and the per-transaction rebind. It fails, naming
// the offending field, if any statement is nil after New (a missed
// preparation) or inside a transaction (a missed rebind -- otherwise a
// latent nil-pointer panic the first time that statement runs under a
// transaction, and only under a transaction).
func TestPreparedStatementsPreparedAndReboundInTx(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := lock.Run(context.Background(), filepath.Join(dir, ".lock"), func(ctx context.Context, writeLock lock.WriterScope) error {
		store, err := New(ctx, filepath.Join(dir, "store.db"), discardLogger(), writeLock)
		if err != nil {
			return err
		}
		defer store.Close()

		assertAllStatementsPrepared(t, store.(*sqliteStore), "after New")

		return store.(*sqliteStore).RunInTransaction(ctx, "stmt-invariant", func(tx platform.Store) error {
			assertAllStatementsPrepared(t, tx.(*sqliteStore), "inside transaction")
			return nil
		})
	})
	require.NoError(t, err)
}
