-- +goose Up
-- +goose StatementBegin
-- Initial schema: the baseline every database starts from. Later
-- schema changes ship as 0002_*.sql, 0003_*.sql, and so on. Every
-- statement is IF NOT EXISTS so that a database already carrying this
-- schema adopts the migration framework on first open without
-- re-creating anything.
--
-- Schema for bpfman SQLite database
--
-- This schema uses the registry + detail tables pattern for links,
-- providing both polymorphic access and type-specific constraints.
--
-- Entity lifecycle
-- ================
--
-- Programs (managed_programs)
-- ---------------------------
--
-- CREATE: A row is inserted only after a BPF program has been
--   successfully loaded into the kernel. There are no intermediate
--   reservation or loading states. The program_id is the
--   kernel-assigned BPF program ID.
--
-- UPDATE: Programs may be updated to change metadata, ownership, or
--   map relationships. The map_owner_id self-reference allows
--   multiple programs to share BPF maps with one designated owner.
--
-- DELETE: Deleting a program cascades through the entire link
--   hierarchy:
--
--     DELETE managed_programs
--       -> CASCADE to links (via kernel_prog_id FK)
--         -> CASCADE to link_*_details (via id FK)
--
--   A single DELETE on managed_programs cleans up the base link row
--   and its type-specific detail row automatically.
--
--   Exception: if the program is a map owner (another program's
--   map_owner_id points to it), the delete is RESTRICTED. You must
--   delete the dependent programs first.
--
-- Links (links + link_*_details)
-- ------------------------------
--
-- The links table is a polymorphic registry. Every link gets a row
-- here regardless of type, with a "kind" discriminator column that
-- indicates which detail table holds the type-specific data. Each
-- detail table has a 1:1 relationship with links, joined on id.
-- This avoids a single wide nullable table and lets each type enforce
-- its own constraints.
--
-- CREATE: A link row is inserted into both the base links table and
--   the appropriate detail table in a single transaction. The id is
--   the bpfman-owned management handle. kernel_link_id is populated
--   only when bpfman captured a kernel bpf_link ID for the attachment.
--
-- UPDATE: Non-dispatcher detail rows are generally stable after
--   creation. Dispatcher-backed link rows are replaced as part of
--   whole-snapshot dispatcher updates rather than being mutated
--   individually. The base links row is generally immutable after
--   creation.
--
-- DELETE: Deleting a link row cascades to its detail table row.
--   Links are also deleted automatically when their parent program is
--   deleted (see program deletion above).
--
-- Dispatchers (dispatchers)
-- -------------------------
--
-- XDP and TC do not natively support multiple programs on one
-- interface, so bpfman uses dispatcher BPF programs to chain them.
-- There is exactly one dispatcher per (type, nsid, ifindex) tuple.
--
-- CREATE: A dispatcher row is inserted when the first extension
--   program is attached to an interface. The dispatcher's program_id
--   is the kernel-assigned ID of the dispatcher BPF program itself.
--   There is deliberately no FK back to managed_programs, giving
--   flexibility in lifecycle ordering (the dispatcher row may be
--   created before or after the corresponding managed_programs row).
--
-- UPDATE: When a dispatcher is recompiled, the store replaces the
--   entire snapshot: old extension link rows are deleted, the
--   dispatcher row is upserted with a new revision and program_id,
--   and new extension link rows are inserted.
--
-- DELETE: Removing a dispatcher does not automatically cascade to
--   extension link detail rows. The snapshot-based store methods
--   (DeleteDispatcherSnapshot, ReplaceDispatcherSnapshot) explicitly
--   delete extension link records by attach point before removing
--   the dispatcher row. This gives the store full control over
--   cleanup ordering.
--
-- TCX (link_tcx_details)
-- ----------------------
--
-- TCX is a special case: the kernel handles multi-program ordering
-- natively, so no dispatcher is needed. TCX detail rows have no
-- dispatcher_program_id, no position column, and no dispatcher
-- cascade behaviour. They are cleaned up solely by the links cascade.
--
-- Foreign key actions reference
-- =============================
--
-- ON DELETE CASCADE: when the referenced (parent) row is deleted,
--   automatically delete all rows that reference it. Used here so
--   that deleting a program removes its links, and deleting a link
--   removes its detail row. The cascade can chain: deleting a
--   managed_programs row cascades to links, which cascades to
--   link_*_details.
--
-- ON DELETE RESTRICT: prevent the delete entirely if any row still
--   references the target. The delete statement fails with an error.
--   Used on map_owner_id so that a map-owning program cannot be
--   removed while dependent programs still exist.
--
-- ON UPDATE CASCADE: when the referenced column value in the parent
--   row changes, automatically update the FK column in all
--   referencing rows to match.
--
-- STRICT: a SQLite table mode that enforces column types. Without
--   it, SQLite allows any value in any column regardless of declared
--   type. With STRICT, inserting a TEXT into an INTEGER column (or
--   vice versa) is an error. Every table in this schema uses STRICT.
--
-- CHECK: an inline constraint that validates a value at
--   insert/update time. Used throughout for enum-style columns
--   (program_type, kind, direction), range constraints (offset >= 0,
--   position BETWEEN 0 AND 9), boolean columns (IN (0, 1)), and
--   JSON validation (json_valid).

CREATE TABLE IF NOT EXISTS map_sets (
    id         INTEGER PRIMARY KEY,
    pin_path   TEXT NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

-- Programs table for managed BPF programs
-- A row exists only after successful load - no reservation/loading states.
-- Schema is normalised: individual columns for queryable fields, JSON only for opaque data.
CREATE TABLE IF NOT EXISTS managed_programs (
    program_id INTEGER PRIMARY KEY,
    program_name TEXT NOT NULL,
    program_type TEXT NOT NULL CHECK (program_type IN (
        'xdp','tc','tcx','tracepoint','kprobe','kretprobe',
        'uprobe','uretprobe','fentry','fexit'
    )),
    object_path TEXT NOT NULL,
    source_path TEXT,            -- the caller's file-load path operand,
                                     -- verbatim; NULL for image loads, whose
                                     -- provenance lives in image_source
    pin_path TEXT NOT NULL,
    attach_func TEXT,
    global_data TEXT CHECK (global_data IS NULL OR json_valid(global_data)),
                                     -- JSON map<string, bytes>, opaque
    map_set_id INTEGER NOT NULL,
    image_source TEXT CHECK (
        image_source IS NULL
        OR (
            json_valid(image_source)
            AND json_extract(image_source, '$.url') IS NOT NULL
            AND json_extract(image_source, '$.url') != ''
            AND json_extract(image_source, '$.pull_policy') IN (
                'Always', 'IfNotPresent', 'Never'
            )
        )
    ),           -- JSON ImageSource struct, NULL if file-loaded
    owner TEXT,
    description TEXT,
    license TEXT,                -- ELF license string from bytecode
    gpl_compatible INTEGER NOT NULL DEFAULT 0 CHECK (gpl_compatible IN (0, 1)),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
                                     -- User key-value metadata as JSON
    created_at TEXT NOT NULL,
    updated_at TEXT,
    -- updated_at is NULL when the program has never been updated
    -- since creation, distinct from CreatedAt. The bpfman shape
    -- contract surfaces this as JSON null so "created at T, never
    -- updated" and "created at T, updated at T'" stay
    -- distinguishable on the wire.

    FOREIGN KEY (map_set_id)
        REFERENCES map_sets(id)
        ON DELETE RESTRICT
) STRICT;

-- Note: No uniqueness constraint on bpfman.io/ProgramName.
-- Multiple programs can share the same application name (e.g., when loading
-- multiple BPF programs from a single image via the operator).

--------------------------------------------------------------------------------
-- Links Table (Polymorphic Core)
--------------------------------------------------------------------------------

-- links contains all common fields for managed links.
-- id is the bpfman-managed attachment handle. kernel_link_id is the
-- captured kernel bpf_link ID, if the attach path observed one.
CREATE TABLE IF NOT EXISTS links (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN (
                        'tracepoint','kprobe','kretprobe','uprobe','uretprobe',
                        'fentry','fexit','xdp','tc','tcx'
                    )),        -- LinkKind discriminator
    kernel_prog_id  INTEGER NOT NULL,     -- useful for queries
    kernel_link_id  INTEGER,
    pin_path        TEXT,
    metadata_json   TEXT NOT NULL DEFAULT '{}'
                        CHECK (json_valid(metadata_json)),  -- user key/value labels
    created_at      TEXT NOT NULL,

    -- Deleting a program cascades here, removing all its links.
    -- This in turn cascades to the type-specific detail tables.
    FOREIGN KEY (kernel_prog_id)
        REFERENCES managed_programs(program_id)
        ON DELETE CASCADE
) STRICT;

CREATE INDEX IF NOT EXISTS idx_links_by_prog ON links(kernel_prog_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_links_kernel_link_id
    ON links(kernel_link_id)
    WHERE kernel_link_id IS NOT NULL;

-- Pin paths are deterministic per attachment key (e.g. TCX:
-- direction, nsid, ifindex, program), so two records sharing one
-- pin is the corrupted two-records-one-pin state the manager's
-- duplicate-attach rejection prevents. The index makes that state
-- unrepresentable even if a future code path skips the manager
-- check. Ephemeral links carry NULL pins and are exempt.
CREATE UNIQUE INDEX IF NOT EXISTS idx_links_pin_path
    ON links(pin_path)
    WHERE pin_path IS NOT NULL;

--------------------------------------------------------------------------------
-- Type-Specific Detail Tables
--------------------------------------------------------------------------------
-- Each link kind has a 1:1 detail table joined on id. This avoids a
-- single wide nullable table; each detail table contains only the columns
-- relevant to its type. All detail tables cascade on delete from links,
-- which in turn cascades from managed_programs.

-- Tracepoint links
CREATE TABLE IF NOT EXISTS link_tracepoint_details (
    id INTEGER PRIMARY KEY,
    tp_group TEXT NOT NULL,
    tp_name TEXT NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

-- Kprobe/Kretprobe links
CREATE TABLE IF NOT EXISTS link_kprobe_details (
    id INTEGER PRIMARY KEY,
    fn_name TEXT NOT NULL,
    offset INTEGER NOT NULL DEFAULT 0 CHECK (offset >= 0),
    retprobe INTEGER NOT NULL DEFAULT 0 CHECK (retprobe IN (0, 1)),

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

-- Uprobe/Uretprobe links
CREATE TABLE IF NOT EXISTS link_uprobe_details (
    id INTEGER PRIMARY KEY,
    target TEXT NOT NULL,
    fn_name TEXT,
    offset INTEGER NOT NULL DEFAULT 0 CHECK (offset >= 0),
    pid INTEGER,
    container_pid INTEGER,
    retprobe INTEGER NOT NULL DEFAULT 0 CHECK (retprobe IN (0, 1)),

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

-- Fentry links
CREATE TABLE IF NOT EXISTS link_fentry_details (
    id INTEGER PRIMARY KEY,
    fn_name TEXT NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

-- Fexit links
CREATE TABLE IF NOT EXISTS link_fexit_details (
    id INTEGER PRIMARY KEY,
    fn_name TEXT NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

--------------------------------------------------------------------------------
-- Dispatchers
--------------------------------------------------------------------------------

-- Dispatchers for XDP/TC multi-program chaining.
--
-- Natural key (type, nsid, ifindex) is the primary key - this is how
-- the system identifies a dispatcher ("the XDP dispatcher for this
-- interface"). program_id is a runtime fact that changes on every
-- rebuild; it has a UNIQUE constraint solely so that member detail
-- rows (link_xdp_details, link_tc_details) can reference the
-- currently persisted dispatcher revision via FK. It is not the
-- logical identity of the dispatcher.
--
-- No FK back to managed_programs: this is deliberate, giving
-- flexibility in lifecycle ordering (the dispatcher row may be
-- created before or after the corresponding managed_programs row).
CREATE TABLE IF NOT EXISTS dispatchers (
    type TEXT NOT NULL CHECK (type IN ('xdp', 'tc-ingress', 'tc-egress')),
    nsid INTEGER NOT NULL,
    ifindex INTEGER NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    program_id INTEGER NOT NULL UNIQUE,
    kernel_link_id INTEGER,
    priority INTEGER CHECK (priority >= 0),
    filter_handle INTEGER CHECK (filter_handle >= 0),
    netns TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,

    PRIMARY KEY (type, nsid, ifindex),

    -- XDP dispatchers have a kernel link but no filter priority or handle.
    -- TC dispatchers have a filter priority and the exact kernel-assigned
    -- filter handle (recorded at create), but no kernel link (they use
    -- netlink filters).
    CHECK (
        (type = 'xdp' AND kernel_link_id IS NOT NULL AND priority IS NULL AND filter_handle IS NULL)
        OR
        (type IN ('tc-ingress', 'tc-egress') AND kernel_link_id IS NULL AND priority IS NOT NULL AND filter_handle IS NOT NULL)
    )
) STRICT;

--------------------------------------------------------------------------------
-- Dispatcher Extension Detail Tables
--------------------------------------------------------------------------------

-- XDP links (dispatcher-based)
-- Revision is not stored here; it is a snapshot-header fact owned by
-- the dispatchers table. Read paths JOIN to dispatchers to derive it.
CREATE TABLE IF NOT EXISTS link_xdp_details (
    id INTEGER PRIMARY KEY,
    interface TEXT NOT NULL,
    ifindex INTEGER NOT NULL,
    priority INTEGER NOT NULL CHECK (priority >= 0),
    position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 9),
    proceed_on TEXT NOT NULL CHECK (json_valid(proceed_on)),
    netns TEXT,
    nsid INTEGER NOT NULL,
    dispatcher_program_id INTEGER NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE,
    FOREIGN KEY (dispatcher_program_id)
        REFERENCES dispatchers(program_id)
) STRICT;

-- Enforce unique position per interface in namespace
CREATE UNIQUE INDEX IF NOT EXISTS uq_xdp_dispatcher_position
    ON link_xdp_details(nsid, ifindex, position);

-- Attach-point index for snapshot queries
CREATE INDEX IF NOT EXISTS idx_link_xdp_by_attach_point
    ON link_xdp_details(nsid, ifindex);

-- TC links (dispatcher-based)
-- Revision is not stored here; it is a snapshot-header fact owned by
-- the dispatchers table. Read paths JOIN to dispatchers to derive it.
CREATE TABLE IF NOT EXISTS link_tc_details (
    id INTEGER PRIMARY KEY,
    interface TEXT NOT NULL,
    ifindex INTEGER NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('ingress', 'egress')),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 9),
    proceed_on TEXT NOT NULL CHECK (json_valid(proceed_on)),
    netns TEXT,
    nsid INTEGER NOT NULL,
    dispatcher_program_id INTEGER NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE,
    FOREIGN KEY (dispatcher_program_id)
        REFERENCES dispatchers(program_id)
) STRICT;

-- Enforce unique position per interface + direction in namespace
CREATE UNIQUE INDEX IF NOT EXISTS uq_tc_dispatcher_position
    ON link_tc_details(nsid, ifindex, direction, position);

-- Attach-point index for snapshot queries
CREATE INDEX IF NOT EXISTS idx_link_tc_by_attach_point
    ON link_tc_details(nsid, ifindex, direction);

-- TCX links (kernel multi-attach)
-- The kernel handles multi-program ordering natively for TCX, so no
-- dispatcher is needed. No dispatcher_program_id, no position column,
-- no dispatcher cascade behaviour. Cleaned up solely by the links
-- cascade from managed_programs.
CREATE TABLE IF NOT EXISTS link_tcx_details (
    id INTEGER PRIMARY KEY,
    interface TEXT NOT NULL,
    ifindex INTEGER NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('ingress', 'egress')),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    netns TEXT,
    nsid INTEGER NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

--------------------------------------------------------------------------------
-- Shared Map Pins (reference counting for PinByName maps)
--------------------------------------------------------------------------------

-- Tracks which programs use shared PinByName map pins. When the last
-- program referencing a shared map is unloaded, the shared pin under
-- {bpffs}/shared/{map_name} is removed. ON DELETE CASCADE ensures
-- entries are cleaned up if a program is deleted from managed_programs
-- without an explicit cleanup step (e.g., during GC after a crash).
CREATE TABLE IF NOT EXISTS shared_map_pins (
    map_name TEXT NOT NULL,
    program_id INTEGER NOT NULL,
    PRIMARY KEY (map_name, program_id),
    FOREIGN KEY (program_id)
        REFERENCES managed_programs(program_id)
        ON DELETE CASCADE
) STRICT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop every object the baseline creates. Child tables first so the
-- foreign-key references unwind cleanly; the per-table indexes go with
-- their tables. Production never runs this -- the store rolls forward,
-- never down -- but a complete Down keeps the baseline reversible for
-- tests and ad-hoc inspection.
DROP TABLE IF EXISTS shared_map_pins;
DROP TABLE IF EXISTS link_tcx_details;
DROP TABLE IF EXISTS link_tc_details;
DROP TABLE IF EXISTS link_xdp_details;
DROP TABLE IF EXISTS dispatchers;
DROP TABLE IF EXISTS link_fexit_details;
DROP TABLE IF EXISTS link_fentry_details;
DROP TABLE IF EXISTS link_uprobe_details;
DROP TABLE IF EXISTS link_kprobe_details;
DROP TABLE IF EXISTS link_tracepoint_details;
DROP TABLE IF EXISTS links;
DROP TABLE IF EXISTS managed_programs;
DROP TABLE IF EXISTS map_sets;
-- +goose StatementEnd
