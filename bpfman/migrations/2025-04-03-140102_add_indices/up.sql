-- SPDX-License-Identifier: Apache-2.0
-- Copyright Authors of bpfman

-- Index for quick lookups of all maps used by programs.
CREATE INDEX idx_program_maps_map_id ON bpf_program_maps(map_id);

-- Index for querying all links associated with a program.
CREATE INDEX idx_links_program_id ON bpf_links(program_id);

-- Index to filter or group programs by kind.
CREATE INDEX idx_programs_kind ON bpf_programs(kind);

-- Index to filter programs by state (e.g. loaded).
CREATE INDEX idx_programs_state ON bpf_programs(state);

-- Index for kernel program type lookups.
CREATE INDEX idx_programs_kernel_type ON bpf_programs(kernel_program_type);

-- Index for fast lookups by map name.
CREATE INDEX idx_maps_name ON bpf_maps(name);

-- Index for fast lookups by program name.
CREATE INDEX idx_programs_name ON bpf_programs(name);
