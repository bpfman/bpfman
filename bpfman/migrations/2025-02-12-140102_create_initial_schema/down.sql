-- SPDX-License-Identifier: Apache-2.0
-- Copyright Authors of bpfman

-- This file should undo anything in `up.sql`.
DROP TABLE IF EXISTS bpf_links;
DROP TABLE IF EXISTS bpf_maps;
DROP TABLE IF EXISTS bpf_program_maps;
DROP TABLE IF EXISTS bpf_programs;
