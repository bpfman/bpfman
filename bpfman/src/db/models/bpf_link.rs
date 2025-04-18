// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use chrono::NaiveDateTime;
use diesel::prelude::*;

use crate::db::KernelU32;

/// A persisted record for a BPF link that associates a program with a target.
#[derive(Debug, AsChangeset, Insertable, Identifiable, Queryable)]
#[diesel(belongs_to(BpfProgram, foreign_key = program_id))]
#[diesel(table_name = crate::db::bpf_links)]
#[diesel(primary_key(id))]
pub struct BpfLink {
    pub id: KernelU32,
    pub program_id: KernelU32,
    pub link_type: Option<String>,
    pub target: Option<String>,
    pub state: String,
    pub created_at: NaiveDateTime,
    pub updated_at: Option<NaiveDateTime>,
}

impl BpfLink {
    /// Inserts a new BPF link record.
    pub fn insert_record(conn: &mut SqliteConnection, link: &BpfLink) -> QueryResult<()> {
        diesel::insert_into(crate::db::bpf_links::table)
            .values(link)
            .execute(conn)?;
        Ok(())
    }
}

#[cfg(test)]
impl Default for BpfLink {
    fn default() -> Self {
        Self {
            id: Default::default(),
            program_id: Default::default(),
            link_type: None,
            target: None,
            state: "".to_owned(),
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}
