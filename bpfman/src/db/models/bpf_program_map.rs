// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! BpfProgramMap represents the many-to-many relationship between
//! BPF programs and BPF maps.

use diesel::prelude::*;

use crate::db::KernelU32;

#[derive(Debug, Queryable, Selectable, Associations)]
#[diesel(belongs_to(crate::db::BpfProgram, foreign_key = program_id))]
#[diesel(belongs_to(crate::db::BpfMap, foreign_key = map_id))]
#[diesel(table_name = crate::db::bpf_program_maps)]
pub struct BpfProgramMap {
    pub program_id: KernelU32,
    pub map_id: KernelU32,
}

impl BpfProgramMap {
    /// Inserts a new mapping between a BPF program and a BPF map.
    ///
    /// This establishes an entry in the join table linking the two
    /// resources. The caller must ensure both program and map IDs
    /// exist.
    pub fn insert_record(
        conn: &mut SqliteConnection,
        program_id: KernelU32,
        map_id: KernelU32,
    ) -> Result<(), diesel::result::Error> {
        use crate::db::bpf_program_maps;

        diesel::insert_into(bpf_program_maps::table)
            .values((
                bpf_program_maps::program_id.eq(program_id),
                bpf_program_maps::map_id.eq(map_id),
            ))
            .execute(conn)?;

        Ok(())
    }
}
