#!/usr/bin/env bash

set -e

if [ $# -ne 1 ]; then
    echo "Usage: ${0##*/} /path/to/sqlite.db"
    exit 1
fi

db="$1"

if [ ! -f "$db" ]; then
    echo "Error: Database file '$db' does not exist." >&2
    exit 1
fi

sqlite3 "$db" <<EOF
.headers on
.mode column
SELECT id, name, kind, map_pin_path, kernel_loaded_at FROM bpf_programs ORDER BY created_at;
EOF

sqlite3 "$db" <<EOF
.headers on
.mode column
SELECT * FROM bpf_maps ORDER BY created_at;
EOF

sqlite3 "$db" <<EOF
.headers on
.mode column
SELECT * FROM bpf_program_maps ORDER BY program_id ASC;
EOF
