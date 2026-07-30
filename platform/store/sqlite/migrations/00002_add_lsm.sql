-- Add the 'lsm' link kind's detail table.
--
-- LSM programs load like fentry/fexit: the attach target (here the LSM
-- hook, e.g. file_open) is fixed at load time and stored in
-- managed_programs.attach_func, so no new program column is needed.
-- program_type and links.kind are plain TEXT columns whose permitted
-- values are enforced in the domain, not by a schema CHECK, so adding a
-- new type needs no table rebuild. The only additive change is the
-- link_lsm_details table for the hook name shown on lsm links, 1:1 with
-- links on id and cascading on delete, mirroring link_fexit_details.

-- +goose Up
CREATE TABLE link_lsm_details (
    id INTEGER PRIMARY KEY,
    hook_name TEXT NOT NULL,

    FOREIGN KEY (id)
        REFERENCES links(id)
        ON DELETE CASCADE
) STRICT;

-- +goose Down
DROP TABLE link_lsm_details;
