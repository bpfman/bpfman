-- Add the 'lsm' program type and link kind.
--
-- LSM programs load like fentry/fexit: the attach target (here the LSM
-- hook, e.g. file_open) is fixed at load time and stored in
-- managed_programs.attach_func, so no new program column is needed. This
-- migration only widens the two enum CHECK constraints and adds the
-- link_lsm_details table for the hook name shown on lsm links.
--
-- Widening a CHECK (... IN (...)) is a full table rebuild in SQLite:
-- create the wider table, copy the rows, drop the old, rename the new
-- into place. Both rebuilt tables are foreign-key targets, so the
-- rebuild runs with foreign_keys OFF inside one explicit transaction;
-- foreign_key_check proves nothing was orphaned before enforcement is
-- restored. See TEMPLATE_add_program_type.sql.tmpl for the rationale.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA foreign_keys = OFF;

BEGIN TRANSACTION;

-- (1) Rebuild managed_programs to widen the program_type CHECK. The
--     column list is copied verbatim from 00001_baseline.sql; only the
--     CHECK list gains 'lsm'.
CREATE TABLE managed_programs_new (
    program_id INTEGER PRIMARY KEY,
    program_name TEXT NOT NULL,
    program_type TEXT NOT NULL CHECK (program_type IN (
        'xdp','tc','tcx','tracepoint','kprobe','kretprobe',
        'uprobe','uretprobe','fentry','fexit','lsm'
    )),
    object_path TEXT NOT NULL,
    source_path TEXT,
    pin_path TEXT NOT NULL,
    attach_func TEXT,
    global_data TEXT CHECK (global_data IS NULL OR json_valid(global_data)),
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
    ),
    owner TEXT,
    description TEXT,
    license TEXT,
    gpl_compatible INTEGER NOT NULL DEFAULT 0 CHECK (gpl_compatible IN (0, 1)),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at TEXT NOT NULL,
    updated_at TEXT,

    FOREIGN KEY (map_set_id)
        REFERENCES map_sets(id)
        ON DELETE RESTRICT
) STRICT;

INSERT INTO managed_programs_new SELECT * FROM managed_programs;
DROP TABLE managed_programs;
ALTER TABLE managed_programs_new RENAME TO managed_programs;

-- (2) Rebuild links to widen the kind CHECK, then recreate its indexes
--     (they were dropped with the old table).
CREATE TABLE links_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN (
                        'tracepoint','kprobe','kretprobe','uprobe','uretprobe',
                        'fentry','fexit','xdp','tc','tcx','lsm'
                    )),
    kernel_prog_id  INTEGER NOT NULL,
    kernel_link_id  INTEGER,
    pin_path        TEXT,
    metadata_json   TEXT NOT NULL DEFAULT '{}'
                        CHECK (json_valid(metadata_json)),
    created_at      TEXT NOT NULL,

    FOREIGN KEY (kernel_prog_id)
        REFERENCES managed_programs(program_id)
        ON DELETE CASCADE
) STRICT;

INSERT INTO links_new SELECT * FROM links;
DROP TABLE links;
ALTER TABLE links_new RENAME TO links;

CREATE INDEX idx_links_by_prog ON links(kernel_prog_id);
CREATE UNIQUE INDEX idx_links_kernel_link_id
    ON links(kernel_link_id)
    WHERE kernel_link_id IS NOT NULL;
CREATE UNIQUE INDEX idx_links_pin_path
    ON links(pin_path)
    WHERE pin_path IS NOT NULL;

-- (3) Add the lsm link detail table: 1:1 with links on id, cascading on
--     delete, mirroring link_fexit_details.
CREATE TABLE link_lsm_details (
    id INTEGER PRIMARY KEY,
    hook_name TEXT NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

PRAGMA foreign_key_check;

COMMIT;

PRAGMA foreign_keys = ON;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA foreign_keys = OFF;

BEGIN TRANSACTION;

DROP TABLE link_lsm_details;

DELETE FROM links WHERE kind = 'lsm';

CREATE TABLE links_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN (
                        'tracepoint','kprobe','kretprobe','uprobe','uretprobe',
                        'fentry','fexit','xdp','tc','tcx'
                    )),
    kernel_prog_id  INTEGER NOT NULL,
    kernel_link_id  INTEGER,
    pin_path        TEXT,
    metadata_json   TEXT NOT NULL DEFAULT '{}'
                        CHECK (json_valid(metadata_json)),
    created_at      TEXT NOT NULL,

    FOREIGN KEY (kernel_prog_id)
        REFERENCES managed_programs(program_id)
        ON DELETE CASCADE
) STRICT;

INSERT INTO links_new SELECT * FROM links;
DROP TABLE links;
ALTER TABLE links_new RENAME TO links;

CREATE INDEX idx_links_by_prog ON links(kernel_prog_id);
CREATE UNIQUE INDEX idx_links_kernel_link_id
    ON links(kernel_link_id)
    WHERE kernel_link_id IS NOT NULL;
CREATE UNIQUE INDEX idx_links_pin_path
    ON links(pin_path)
    WHERE pin_path IS NOT NULL;

DELETE FROM managed_programs WHERE program_type = 'lsm';

CREATE TABLE managed_programs_new (
    program_id INTEGER PRIMARY KEY,
    program_name TEXT NOT NULL,
    program_type TEXT NOT NULL CHECK (program_type IN (
        'xdp','tc','tcx','tracepoint','kprobe','kretprobe',
        'uprobe','uretprobe','fentry','fexit'
    )),
    object_path TEXT NOT NULL,
    source_path TEXT,
    pin_path TEXT NOT NULL,
    attach_func TEXT,
    global_data TEXT CHECK (global_data IS NULL OR json_valid(global_data)),
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
    ),
    owner TEXT,
    description TEXT,
    license TEXT,
    gpl_compatible INTEGER NOT NULL DEFAULT 0 CHECK (gpl_compatible IN (0, 1)),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at TEXT NOT NULL,
    updated_at TEXT,

    FOREIGN KEY (map_set_id)
        REFERENCES map_sets(id)
        ON DELETE RESTRICT
) STRICT;

INSERT INTO managed_programs_new SELECT * FROM managed_programs;
DROP TABLE managed_programs;
ALTER TABLE managed_programs_new RENAME TO managed_programs;

PRAGMA foreign_key_check;

COMMIT;

PRAGMA foreign_keys = ON;
-- +goose StatementEnd
