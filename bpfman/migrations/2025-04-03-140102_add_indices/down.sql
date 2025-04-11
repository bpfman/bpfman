-- SPDX-License-Identifier: Apache-2.0
-- Copyright Authors of bpfman

-- Drop indexes added for performance improvements.

DROP INDEX IF EXISTS idx_program_maps_map_id;
DROP INDEX IF EXISTS idx_links_program_id;
DROP INDEX IF EXISTS idx_programs_kind;
DROP INDEX IF EXISTS idx_programs_state;
DROP INDEX IF EXISTS idx_programs_kernel_type;
DROP INDEX IF EXISTS idx_maps_name;
DROP INDEX IF EXISTS idx_programs_name;
