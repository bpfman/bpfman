// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use chrono::NaiveDateTime;
use diesel::prelude::*;

use crate::db::KernelU32;

#[derive(
    Debug,
    Clone,
    Eq,
    PartialEq,
    serde::Serialize,
    serde::Deserialize,
    AsChangeset,
    Insertable,
    Identifiable,
    Queryable,
)]
#[diesel(table_name = crate::db::bpf_maps)]
#[diesel(primary_key(id))]
pub struct BpfMap {
    pub id: KernelU32,
    pub name: String,
    pub map_type: Option<String>,
    pub key_size: KernelU32,
    pub value_size: KernelU32,
    pub max_entries: KernelU32,
    pub created_at: NaiveDateTime,
    pub updated_at: Option<NaiveDateTime>,
}

impl BpfMap {
    /// Inserts a map record into the database, ignoring conflicts if
    /// the record already exists.
    ///
    /// This method is particularly useful when dealing with shared
    /// maps between multiple programs. If a map with the same ID
    /// already exists in the database, the insertion is silently
    /// skipped without raising an error.
    ///
    /// # Arguments
    ///
    /// * `conn` - A mutable reference to an active SQLite connection
    /// * `map` - The BpfMap record to insert
    ///
    /// # Returns
    ///
    /// * `QueryResult<usize>` - On success, returns the number of rows affected (1 if inserted, 0 if skipped).
    /// * On failure, returns a Diesel error (e.g., for connection
    ///   issues or constraint violations).
    pub fn insert_record_on_conflict_do_nothing(
        conn: &mut SqliteConnection,
        map: &BpfMap,
    ) -> QueryResult<usize> {
        diesel::insert_into(crate::db::bpf_maps::table)
            .values(map)
            .on_conflict_do_nothing()
            .execute(conn)
    }
}

#[cfg(test)]
impl Default for BpfMap {
    fn default() -> Self {
        Self {
            id: 0u32.into(),
            name: "".to_owned(),
            map_type: None,
            key_size: 0u32.into(),
            value_size: 0u32.into(),
            max_entries: 0u32.into(),
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}
