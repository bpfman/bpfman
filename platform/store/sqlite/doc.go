// Package sqlite implements [platform.Store] using SQLite.
//
// # Overview
//
// This package provides the concrete database layer for bpfman's
// program, link, and dispatcher metadata. It implements all the
// narrow store interfaces defined in platform/ (ProgramReader,
// ProgramWriter, LinkWriter, DispatcherStore, etc.)
// against a single SQLite database.
//
// # Schema
//
// The schema uses a polymorphic registry pattern for links: a links
// table with a kind discriminator column and separate detail tables
// per link type (link_tracepoint_details, link_kprobe_details,
// link_xdp_details, link_tc_details, link_tcx_details,
// link_uprobe_details, link_fentry_details, link_fexit_details).
// Programs store user metadata as a JSON column (metadata_json).
//
// # Schema Versioning and Migrations
//
// The schema is versioned and evolved by ordered, forward-only
// migrations under migrations/, applied with goose
// (github.com/pressly/goose/v3). Each migration is a
// migrations/NNNNN_*.sql file carrying goose Up/Down annotations; the
// numeric prefix is the schema version the up section advances the
// database to, and goose records applied versions in its own
// goose_db_version table.
//
// Opening a store applies every pending migration in order; a database
// already at the latest version is untouched. A database newer than the
// running build understands is refused, never downgraded and never
// deleted. The lowest migration, 00001_baseline.sql, is the initial
// schema: every statement is idempotent (CREATE TABLE/INDEX IF NOT
// EXISTS) so that a database already carrying this schema -- one written
// by a pre-migration build, with no goose_db_version table -- adopts
// the migration framework on next open without losing data.
//
// goose wraps normal migrations in a transaction. A migration that
// needs SQLite table-rebuild behaviour outside that wrapper marks
// itself with -- +goose NO TRANSACTION and manages its own explicit
// transaction; see TEMPLATE_add_program_type.sql.tmpl.
//
// # Driver
//
// The store uses github.com/mattn/go-sqlite3, a cgo database/sql
// driver that links the SQLite C library. CGO_ENABLED=1 is required
// regardless (the bpfman-ns transport uses a C constructor), and
// binaries link either statically or dynamically. The build never
// loads SQLite runtime extensions and compiles with
// sqlite_omit_load_extension, which keeps a static binary free of a
// dlopen reference.
//
// # Calling Conventions
//
// Individual methods execute against s.conn, which may be either the
// underlying *sql.DB (autocommit mode) or a *sql.Tx (transactional
// mode).
//
// Store methods are atomic domain primitives. platform.Store is the
// persistence boundary for bpfman domain operations, not a statement
// executor: a method named ReplaceDispatcherSnapshot means "replace
// the snapshot", not "run one fragment of a replacement and hope the
// caller supplied atomicity". A method that needs several SQL
// statements to honour its name therefore owns a transaction
// internally: CreateLink (registry row plus detail row),
// CreatePendingLink (createLink plus the pin-path update), and the
// dispatcher snapshot operations (ReplaceDispatcherSnapshot,
// DeleteDispatcherSnapshot). Every caller gets the schema's
// atomicity guarantees without knowing to ask for them.
//
// The visibility split carries the convention: exported store
// methods are safe domain operations and own any transaction needed
// to preserve their own invariants; unexported statement helpers
// (createLink, setLinkPinPath, replaceDispatcherSnapshot,
// deleteDispatcherSnapshot) assume the caller has already chosen the
// transaction boundary and must not be called outside one when they
// perform multi-statement updates.
//
// RunInTransaction is for the other axis: composing several domain
// operations into one larger atomic unit, as load does when it
// commits a batch of program saves together. For example:
//
//	err := store.RunInTransaction(ctx, "example", func(txStore platform.Store) error {
//	    if err := txStore.Save(ctx, id, prog); err != nil {
//	        return err // triggers rollback
//	    }
//	    _, err := txStore.CreateLink(ctx, spec)
//	    return err // commits if nil
//	})
//
// A self-owned transaction entered from inside a RunInTransaction
// callback flattens into the caller's transaction rather than
// beginning a second one, so the two conventions compose.
//
// # Autocommit Behaviour
//
// When methods are called outside a transaction (directly on the
// store), each SQL statement executes in its own implicit transaction
// that commits immediately upon completion. Single-statement methods
// (Get, Save, Delete, List) are therefore atomic by themselves, and
// the multi-statement methods above provide their own transactions.
//
// # WAL Mode and Reader/Writer Implications
//
// The database is opened with WAL (Write-Ahead Logging) mode, which
// provides:
//
//   - Readers do not block writers; writers do not block readers.
//   - A reader sees a consistent snapshot from when its transaction
//     (or statement in autocommit mode) began.
//   - Without an explicit transaction, consecutive reads may see
//     changes from concurrent writers between reads. Use
//     RunInTransaction for consistent multi-read operations.
//
// WAL is load-bearing in this deployment: there is no
// application-level mutex coordinating concurrent access, so the
// server's read RPCs can hit the database while a write transaction
// is in flight. WAL is what keeps readers off the writer's path.
//
// # When to Use RunInTransaction
//
// Use RunInTransaction when you need:
//
//   - Atomicity: all-or-nothing semantics across multiple operations
//   - Consistency: read-your-writes within a sequence of operations
//   - Isolation: a stable view of data across multiple reads
//
// The caller (typically the manager or executor layer) decides
// atomicity requirements based on the operation being performed.
//
// # Concurrency Model
//
// There is no in-process application-level mutex coordinating store
// access. File-backed store open/init requires a lock.WriterScope
// capability, so schema migration, schema creation, and statement
// preparation are serialised by the same cross-process writer flock
// that protects runtime mutations. The bpfman daemon takes that
// flock for mutating RPCs, and mutating CLI invocations take it
// directly.
//
// Within a process, concurrent transactions are coordinated by
// SQLite itself: BeginTx uses the IMMEDIATE transaction type (via
// the _txlock=immediate DSN parameter, see New), so any transaction
// that may write acquires the SQLite writer lock at BeginTx and
// contention waits at a single well-defined point with busy_timeout
// applying cleanly. Read RPCs in the server proceed lockless and
// rely on WAL mode to observe a consistent snapshot without
// blocking writers.
//
// # SQLite Transaction Types
//
// SQLite supports three transaction types, specified at BEGIN:
//
//   - DEFERRED (default): no locks are acquired until the first read
//     or write. A read acquires a SHARED lock (allowing other
//     readers). A write acquires a RESERVED lock (blocking other
//     writers but allowing readers), then an EXCLUSIVE lock at commit
//     time. Risk: a read-then-write transaction may fail at write time
//     if another connection acquired a write lock in between.
//
//   - IMMEDIATE: acquires a RESERVED lock immediately when the
//     transaction begins, blocking other writers but allowing readers.
//     Guarantees that writes will succeed (no "database is locked"
//     errors mid-transaction). Preferred for transactions that will
//     write, but Go's database/sql does not expose this directly.
//
//   - EXCLUSIVE: acquires an EXCLUSIVE lock immediately, blocking all
//     other connections (readers and writers). Rarely needed; mainly
//     useful when you need to guarantee no other connection accesses
//     the database at all.
//
// This implementation uses IMMEDIATE, requested via the
// _txlock=immediate DSN parameter that both supported drivers
// accept. The DEFERRED default would expose a read-then-write
// transaction to a deadlock at the read-to-write upgrade -- sqlite
// breaks the deadlock by returning SQLITE_BUSY_SNAPSHOT
// immediately, bypassing busy_timeout. With IMMEDIATE the wait
// happens at BeginTx where busy_timeout applies cleanly. There is
// no application-level mutex to fall back on.
//
// # Tuning
//
// Two env vars override the contention-recovery knobs. Both are
// consulted at New / NewInMemory time, so they apply to every
// process that opens the store -- the bpfman daemon and every
// bpfman CLI invocation alike, which keeps behaviour symmetric
// across the daemon/CLI split.
//
//   - BPFMAN_SQLITE_BUSY_TIMEOUT: SQLite busy_timeout, the wait
//     budget BeginTx(IMMEDIATE) gives the writer-lock queue
//     before surfacing SQLITE_BUSY. Parsed as a Go duration
//     ("5s", "30s", "500ms"). Default 5s.
//   - BPFMAN_SQLITE_TX_RETRY_BACKOFFS: comma-separated durations
//     ("50ms,200ms,800ms") naming the Go-level retry schedule
//     applied on top of busy_timeout when a transaction still
//     fails with SQLITE_BUSY. The same bounded schedule is also
//     used during file-backed store open/init if migration or
//     statement preparation sees a transient SQLITE_BUSY. Setting
//     the env var to the empty string disables retry entirely.
//     Default "50ms,200ms,800ms".
//
// Invalid values are logged at WARN and the package default is
// used so a misconfigured env never prevents the store from
// opening.
//
// # Prepared Statements
//
// All SQL queries use prepared statements rather than inline SQL
// strings. When a query is executed with an inline string (e.g.,
// db.QueryContext(ctx, "SELECT ...")), SQLite must parse the SQL
// text, validate it, and generate a query plan on every call.
// Prepared statements move this work to initialisation time: the SQL
// is parsed and compiled once, and subsequent executions reuse the
// compiled representation.
//
// Benefits:
//
//   - Reduced CPU overhead: parsing and planning happen once, not
//     per-query
//   - Predictable latency: no parsing jitter during normal operations
//   - Cleaner code: SQL is defined in one place (prepareStatements)
//     rather than scattered across methods
//
// The cost is modest additional complexity in managing statement
// lifecycles, particularly for transactions where tx.StmtContext must
// create transaction-bound handles from the master statements. See
// RunInTransaction for details.
package sqlite
