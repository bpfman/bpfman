-- +goose Up
ALTER TABLE managed_programs
    ADD COLUMN has_xdp_frags INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE managed_programs
    DROP COLUMN has_xdp_frags;
