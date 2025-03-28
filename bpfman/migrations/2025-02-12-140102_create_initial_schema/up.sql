-- SPDX-License-Identifier: Apache-2.0
-- Copyright Authors of bpfman

-- Table for BPF Programs.
--
-- A BPF program is the central object loaded into the kernel. The
-- kernel assigns each program a unique 32-bit (u32) ID. In SQLite,
-- declaring the id as an INTEGER PRIMARY KEY makes it an alias for
-- the rowid. This table stores metadata about the program along with
-- the actual program binary in a BLOB.
CREATE TABLE bpf_programs (
    id INTEGER PRIMARY KEY NOT NULL,
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
    map_owner_id INTEGER,

    -- The program binary. (For our purposes, this is NOT NULL.)
    program_bytes BLOB NOT NULL,

    -- Arbitrary key/value data stored as JSON.
    metadata TEXT NOT NULL DEFAULT '{}',
    global_data TEXT NOT NULL DEFAULT '{}',

    -- Type-specific fields:
    retprobe BOOLEAN,  -- Only for kprobe/uprobe; must be non-null when applicable.
    fn_name TEXT,      -- Only for fentry/fexit; must be non-null when applicable.

    -- Kernel information (populated after the program is loaded into the kernel).
    kernel_name TEXT,
    kernel_program_type INTEGER,
    kernel_loaded_at TEXT,    -- ISO8601 timestamp string
    kernel_tag TEXT,
    kernel_gpl_compatible BOOLEAN,
    kernel_btf_id INTEGER,
    kernel_bytes_xlated INTEGER,
    kernel_jited BOOLEAN,
    kernel_bytes_jited INTEGER,
    kernel_verified_insns INTEGER,
    kernel_bytes_memlock INTEGER,

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Check: if location_type is 'file' then file_path must be provided;
    --       if 'image' then image_url must be provided.
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
    id INTEGER PRIMARY KEY NOT NULL,
    program_id BIGINT NOT NULL REFERENCES bpf_programs(id) ON DELETE CASCADE,
    link_type TEXT,
    target TEXT,
    state TEXT NOT NULL,  -- Expected values: 'pre_attach' or 'attached'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Table for BPF Maps.
--
-- A BPF map stores key-value data that a BPF program may use. A
-- single map can be shared among multiple programs. For this reason,
-- we separate the identity of a map from its association with a
-- program. The bpf_maps table stores one record per unique map (with
-- the kernel's map ID, which is a 32-bit unsigned integer stored as
-- an INTEGER PRIMARY KEY).
CREATE TABLE bpf_maps (
    id INTEGER PRIMARY KEY NOT NULL,  -- Kernel's BPF map ID (u32)
    name TEXT NOT NULL,
    map_type TEXT NOT NULL,
    key_size BIGINT NOT NULL,
    value_size BIGINT NOT NULL,
    max_entries BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Join Table for the Many-to-Many Relationship between Programs and
-- Maps. Because a single BPF map may be shared by multiple
-- programs—and a program may use multiple maps—we need an
-- intermediary table. Each row in this join table represents an
-- association between a program (via program_id) and a map (via
-- map_id). The composite primary key (program_id, map_id) ensures
-- that the association is unique. The ON DELETE CASCADE clauses
-- ensure that if either a program or a map is deleted, the
-- corresponding association is automatically removed.
CREATE TABLE bpf_program_maps (
    program_id BIGINT NOT NULL REFERENCES bpf_programs(id) ON DELETE CASCADE,
    map_id BIGINT NOT NULL REFERENCES bpf_maps(id) ON DELETE CASCADE,
    PRIMARY KEY (program_id, map_id)
);

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
