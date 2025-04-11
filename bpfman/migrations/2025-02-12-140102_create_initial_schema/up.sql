-- SPDX-License-Identifier: Apache-2.0
-- Copyright Authors of bpfman

-- =============================================================================
-- bpfman Database Schema: Design Notes and Guidelines
-- =============================================================================
--
-- This schema is designed to store and manage metadata for eBPF programs,
-- links, and maps. It also enforces referential integrity via foreign key
-- constraints (ensure that `PRAGMA foreign_keys = ON;` is set for each
-- session).
--
-- --- Kernel IDs and Unsigned Integers ---
--
-- Kernel interfaces (via Aya) commonly return unsigned 32-bit values
-- (u32). In this schema, these values are stored in BIGINT columns.
-- This is safe because:
--
--   1. A u32 value (0 to 4,294,967,295) can be represented within a
--      64-bit signed integer (i64) without loss of precision.
--   2. All kernel IDs (and other identifiers) are handled as i64 in the
--      database.
--
-- In the Rust code, a custom wrapper type, `KernelU32`, is used to
-- represent kernel-provided u32 values. The `KernelU32` type provides:
--
--   - Runtime type safety by checking at runtime when converting from i64
--     to u32, ensuring that the value does not exceed u32::MAX.
--
--   - Diesel integration by implementing the necessary traits (e.g.,
--     `ToSql<BigInt, Sqlite>` and `FromSql<BigInt, Sqlite>`) so that it
--     can be used directly with Diesel.
--
-- When designing tables that need to store kernel u32 values (such as program
-- IDs, map owner IDs, or other kernel-related fields), declare the column as
-- BIGINT in the schema and use `KernelU32` in the corresponding Rust structs.
-- For primary keys that originate from the kernel (which are u32), using
-- `KernelU32` is recommended.
--
-- --- Full-Width Unsigned Integers ---
--
-- SQLite's native INTEGER type is a 64-bit signed integer, which is
-- insufficient for Rust's `u64` (or wider) when exact, lossless round-
-- tripping is required. For such values, BLOB columns are used in SQLite.
--
-- In the Rust code, wrapper types (e.g., `U64Blob` and `U128Blob`) are
-- provided that:
--
--   - Convert the primitive into a fixed-length, big-endian byte array on
--     write.
--
--   - Convert back to the primitive on read, failing with a descriptive
--     error if the stored blob's length does not match the expected size.
--
--   - Preserve numeric ordering when compared lexicographically in SQLite.
--
-- Use these wrappers when a full 64-bit (or wider) unsigned integer must be
-- stored.
--
-- --- General Guidelines ---
--
-- * Row IDs and other kernel-provided IDs from Aya are u32 values that are
--   stored as BIGINT (i64) in SQLite, with conversion handled by `KernelU32`.
--
-- * If a full u64 value needs to be represented, declare the column as BLOB
--   and use the appropriate wrapper (e.g., `U64Blob`) in the Rust model.
--
-- * Diesel models should have types that mirror the schema:
--     - BIGINT columns for kernel u32 values (using KernelU32).
--     - BLOB columns for larger unsigned values (using U64Blob, U128Blob, etc.).
--
-- * Ensure that all conversions are safe:
--     - Use `.try_u32()` for fallible conversions from KernelU32 when passing
--       values back to kernel APIs.
--     - Use the methods provided by the wrapper types (e.g., `get()`)
--       to retrieve their inner values.
--
-- --- Example: Representing a Kernel u32 Value ---
--
-- Consider a kernel that returns a u32 value (for example, 1234). In the
-- database, this value is stored in a BIGINT column (i64). In the Rust code,
-- such values are represented using the `KernelU32` wrapper, which enforces
-- runtime safety and provides Diesel integration.
--
-- Database schema snippet:
--
--   CREATE TABLE example_table (
--       id         BIGINT PRIMARY KEY NOT NULL,
--       kernel_id  BIGINT NOT NULL  -- Kernel-provided u32 stored as BIGINT
--   );
--
-- Rust model:
--
--   #[derive(Queryable, Insertable)]
--   #[diesel(table_name = example_table)]
--   pub struct Example {
--       pub id: i64,
--       pub kernel_id: KernelU32,
--   }
--
-- When inserting a new row, a u32 value is converted to KernelU32:
--
--   let example = Example {
--       id: 1,
--       kernel_id: 1234u32.into(),  // Converts u32 to KernelU32 via From<u32>
--   };
--
-- When reading from the database, Diesel converts the BIGINT value into a
-- KernelU32 using the FromSql implementation, ensuring that the value is
-- within the valid u32 range.

-- Enable and enforce foreign key support in SQLite.
PRAGMA foreign_keys = ON;
--
-- NOTE: SQLite does not enforce foreign key constraints unless
--       `PRAGMA foreign_keys` is explicitly enabled. This includes
--       rules like ON DELETE CASCADE and ON UPDATE CASCADE.
--
-- In application code (e.g., via Diesel), this PRAGMA is set at
-- connection time for bpfman clients (see
-- establish_database_connection()).
--
-- But if you're using the SQLite CLI or scripts, foreign key
-- constraints are **disabled by default** and must be enabled
-- manually **per session**:
--
-- Example in the SQLite shell:
--
--     $ sqlite3 /var/lib/bpfman/db.sqlite
--     sqlite> PRAGMA foreign_keys = ON;
--
-- You must run this before executing any statements that rely on
-- foreign key constraints, or else they will be silently ignored.
--
-- Script-friendly check (Bash snippet):
--
--    if [ "$(sqlite3 /var/lib/bpfman/db.sqlite 'PRAGMA foreign_keys')" != "1" ]; then
--        echo "Foreign key constraints are NOT enabled for this session."
--        echo "Add 'PRAGMA foreign_keys = ON;' before executing any SQL."
--        exit 1
--    fi
--
-- Failing to enable this pragma can result in orphaned rows and
-- broken referential integrity — foreign key constraints will not
-- work even though they are declared here in the schema.

-- Table for BPF Programs.
--
-- A BPF program is the central object loaded into the kernel. The
-- kernel assigns each program a unique 32-bit (u32) ID. In SQLite,
-- declaring the id as an BIGINT PRIMARY KEY makes it an alias for
-- the rowid. This table stores metadata about the program along with
-- the actual program binary in a BLOB.
CREATE TABLE bpf_programs (
    id BIGINT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,

    -- Program type discriminator (lowercase).
    kind TEXT NOT NULL
        CHECK(kind IN ('xdp', 'tc', 'tcx', 'tracepoint', 'kprobe', 'uprobe', 'fentry', 'fexit')),

    -- State: whether the program is pre-loaded or loaded
    state TEXT NOT NULL
        CHECK(state IN ('pre_load', 'loaded')),

    -- Location info: the program comes either from a file or an image.
    location_type TEXT NOT NULL
        CHECK(location_type IN ('file', 'image')),
    file_path TEXT,          -- Required if location_type = 'file'
    image_url TEXT,          -- Required if location_type = 'image'
    image_pull_policy TEXT,  -- Only for image-based programs
    username TEXT,           -- Optional for image-based programs
    password TEXT,           -- Optional for image-based programs

    -- Additional location/pinning info.
    map_pin_path TEXT NOT NULL,

    -- Map owner.
    map_owner_id BIGINT,

    -- The program binary. (For our purposes, this is NOT NULL.)
    program_bytes BLOB NOT NULL,

    -- Arbitrary key/value data stored as JSON.
    metadata TEXT,
    global_data TEXT,

    -- Type-specific fields:
    retprobe BOOLEAN,  -- Only for kprobe/uprobe; must be non-null when applicable.
    fn_name TEXT,      -- Only for fentry/fexit; must be non-null when applicable.

    -- Kernel information (populated after the program is loaded into the kernel).
    kernel_name TEXT,
    kernel_program_type BIGINT,
    kernel_loaded_at TEXT,    -- ISO8601 timestamp string
    kernel_tag BLOB NOT NULL,
    kernel_gpl_compatible BOOLEAN,
    kernel_btf_id BIGINT,
    kernel_bytes_xlated BIGINT,
    kernel_jited BOOLEAN,
    kernel_bytes_jited BIGINT,
    kernel_verified_insns BIGINT,
    kernel_bytes_memlock BIGINT,

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP

    -- Check: if location_type is 'file' then file_path must be provided;
    --        if 'image' then image_url must be provided.
    CHECK (
      (location_type = 'file' AND file_path IS NOT NULL)
      OR (location_type = 'image' AND image_url IS NOT NULL)
    ),

    -- Check: if kind is 'fentry' or 'fexit', then fn_name must be provided.
    CHECK (
      (kind IN ('fentry', 'fexit') AND fn_name IS NOT NULL)
      OR (kind NOT IN ('fentry', 'fexit'))
    ),

    -- Check: if kind is 'kprobe' or 'uprobe', then retprobe must be provided.
    CHECK (
      (kind IN ('kprobe', 'uprobe') AND retprobe IS NOT NULL)
      OR (kind NOT IN ('kprobe', 'uprobe'))
    )
);

-- Table for BPF Links.
--
-- A BPF link represents a specific attachment of a program to a
-- target (e.g. a network interface, cgroup, etc.). Although a program
-- may create several links (e.g. attaching to multiple interfaces),
-- each link is an independent object associated with exactly one
-- program. Therefore, we model this as a one-to-many relationship:
-- each link row includes a foreign key referencing its owning
-- program.
CREATE TABLE bpf_links (
    id BIGINT PRIMARY KEY NOT NULL,
    program_id BIGINT NOT NULL REFERENCES bpf_programs(id) ON DELETE CASCADE,
    link_type TEXT,
    target TEXT,
    state TEXT NOT NULL,  -- Expected values: 'pre_attach' or 'attached'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);

-- Table for BPF Maps.
--
-- A BPF map stores key-value data that a BPF program may use. A
-- single map can be shared among multiple programs. For this reason,
-- we separate the identity of a map from its association with a
-- program. The bpf_maps table stores one record per unique map (with
-- the kernel's map ID, which is a 32-bit unsigned integer stored as
-- an BIGINT PRIMARY KEY).
CREATE TABLE bpf_maps (
    id BIGINT PRIMARY KEY NOT NULL,  -- Kernel's BPF map ID (u32)
    name TEXT NOT NULL,
    map_type TEXT,
    key_size BIGINT NOT NULL,
    value_size BIGINT NOT NULL,
    max_entries BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);

-- Join Table for the Many-to-Many Relationship between Programs and
-- Maps.
--
-- A single BPF map may be shared by multiple programs, and a program
-- may use multiple maps. This intermediary table models that
-- relationship. Each row links a program (via program_id) to a map
-- (via map_id), with the composite primary key enforcing uniqueness.
--
-- Deletion behaviour:
-- - If a program is deleted from bpf_programs, all associated rows
--   in this table are automatically removed.
-- - If a map is deleted from bpf_maps, all associated rows in this
--   table are also removed.
--
-- This cascading behaviour is enforced via ON DELETE CASCADE on both
-- foreign keys, ensuring referential integrity without manual
-- cleanup. Note: PRAGMA foreign_keys = ON must be enabled for this to
-- work.
CREATE TABLE bpf_program_maps (
    program_id BIGINT NOT NULL REFERENCES bpf_programs(id) ON DELETE CASCADE,
    map_id     BIGINT NOT NULL REFERENCES bpf_maps(id)     ON DELETE CASCADE,
    PRIMARY KEY (program_id, map_id)
);

-- Trigger to automatically delete unused BPF maps.
--
-- Purpose:
--
-- When a BPF program is deleted, we want to clean up any maps it was
-- using — but only if no other programs are still using those maps.
-- This trigger ensures unused maps are cleaned up automatically.
--
-- Tables involved:
-- - `bpf_programs`:     Metadata for BPF programs.
-- - `bpf_maps`:         Metadata for BPF maps.
-- - `bpf_program_maps`: Join table linking programs to maps.
--
-- This is a many-to-many relationship:
--   - A program may use multiple maps.
--   - A map may be used by multiple programs.
--
-- The join table (i.e., bpf_program_maps) has ON DELETE CASCADE set,
-- so when a program is deleted, any associated rows in
-- `bpf_program_maps` are also deleted. However, maps themselves are
-- not deleted unless **no programs** use them. This trigger enforces
-- that.
--
-- How the trigger works:
--
-- 1. The trigger runs **after** a row is deleted from
--    `bpf_program_maps`. That means some program is no longer using a
--    specific map.
--
-- 2. It checks whether any other rows in `bpf_program_maps` still
--    reference the same map:
--
--       SELECT 1 FROM bpf_program_maps WHERE map_id = OLD.map_id
--
--    This query:
--    - returns **rows** if any other program still uses the map;
--    - returns **nothing** if the map is now unused.
--
--    We wrap this in `NOT EXISTS (...)` to ask:
--      "Is this map now unused?"
--
-- 3. If the answer is yes (no rows found), the trigger deletes the
--    map from `bpf_maps`.
--
-- Why SELECT 1?
-- - `SELECT 1` is a lightweight way to test for existence of rows.
-- - It doesn’t matter what we select; we just care whether any rows exist.
--
-- Real-world examples:
--
--   Case 1: The map is still in use
--   -------------------------------
--     bpf_program_maps:
--       program_id | map_id
--       -----------|-------
--       101        | 300
--       102        | 300
--
--     Deleting program 101 -> removes row (101, 300)
--     SELECT 1 FROM bpf_program_maps WHERE map_id = 300 -> returns row for 102
--     -> NOT EXISTS = false -> map 300 is **not** deleted
--
--   Case 2: The map is now unused
--   -----------------------------
--     bpf_program_maps:
--       program_id | map_id
--       -----------|-------
--       103        | 301
--
--     Deleting program 103 -> removes row (103, 301)
--     SELECT 1 FROM bpf_program_maps WHERE map_id = 301 -> returns nothing
--     -> NOT EXISTS = true -> map 301 **is deleted**
--
-- Why implement this in SQL?
--
-- 1. It avoids putting this logic in application code.
--
-- 2. It guarantees correctness even if multiple programs are deleted
--    or manipulated outside the application (e.g., SQL maintenance
--    using the SQLite shell).
--
-- 3. It’s easier to reason about and maintain at the database level.
--
-- Summary:
--
-- This trigger ensures that `bpf_maps` contains only maps that are
-- still in use by at least one program. It enforces garbage
-- collection of orphaned maps without requiring external application
-- logic. This trigger ensures `bpf_maps` never contains stale, unused
-- maps.
CREATE TRIGGER delete_unused_map_after_program_unlink
AFTER DELETE ON bpf_program_maps
FOR EACH ROW
WHEN NOT EXISTS (
  SELECT 1 FROM bpf_program_maps WHERE map_id = OLD.map_id
)
BEGIN
  DELETE FROM bpf_maps WHERE id = OLD.map_id;
END;

-- Trigger for bpf_programs.
CREATE TRIGGER update_bpf_programs_updated_at
AFTER UPDATE ON bpf_programs
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
  UPDATE bpf_programs
  SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
  WHERE id = NEW.id;
END;

-- Trigger for bpf_links.
CREATE TRIGGER update_bpf_links_updated_at
AFTER UPDATE ON bpf_links
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
  UPDATE bpf_links
  SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
  WHERE id = NEW.id;
END;

-- Trigger for bpf_maps.
CREATE TRIGGER update_bpf_maps_updated_at
AFTER UPDATE ON bpf_maps
FOR EACH ROW
WHEN NEW.updated_at = OLD.updated_at
BEGIN
  UPDATE bpf_maps
  SET updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
  WHERE id = NEW.id;
END;
