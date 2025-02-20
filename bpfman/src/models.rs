// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use chrono::{NaiveDateTime, Utc};
use diesel::prelude::*;

#[derive(
    Debug,
    PartialEq,
    Eq,
    serde::Serialize,
    serde::Deserialize,
    AsChangeset,
    Insertable,
    Identifiable,
    Selectable,
    Queryable,
    QueryableByName,
)]
#[diesel(table_name = crate::schema::bpf_programs)]
pub struct BpfProgram {
    /// Kernel's BPF program ID (alias for rowid).
    pub id: i64,

    /// Program name (NOT NULL).
    pub name: String,

    /// Optional program description.
    pub description: Option<String>,

    /// Program type discriminator in lowercase.
    /// Allowed values: "xdp", "tc", "tcx", "tracepoint", "kprobe", "uprobe", "fentry", "fexit".
    pub kind: String,

    /// Program state: "pre_load" or "loaded"
    pub state: String,

    /// Location type: either "file" or "image"
    pub location_type: String,

    /// For file-based programs; required when location_type = "file"
    pub file_path: Option<String>,

    /// For image-based programs; required when location_type = "image"
    pub image_url: Option<String>,

    /// Image pull policy (optional)
    pub image_pull_policy: Option<String>,

    /// Optional username for image-based authentication.
    pub username: Option<String>,

    /// Optional password for image-based authentication.
    pub password: Option<String>,

    /// Map pin path (NOT NULL)
    pub map_pin_path: String,

    /// Optional map owner ID.
    pub map_owner_id: Option<i32>,

    /// The program binary; NOT NULL.
    #[diesel(sql_type = diesel::sql_types::Binary)]
    pub program_bytes: Vec<u8>,

    /// Arbitrary metadata as a JSON string, defaults to {}.
    pub metadata: String,

    /// Global data as a JSON string, defaults to {}.
    pub global_data: String,

    /// For "kprobe"/"uprobe" types; required when applicable.
    pub retprobe: Option<bool>,

    /// For "fentry"/"fexit" types; required when applicable.
    pub fn_name: Option<String>,

    /// Kernel information: name assigned by the kernel.
    pub kernel_name: Option<String>,

    /// Kernel program type.
    pub kernel_program_type: Option<i32>,

    /// When the program was loaded (ISO8601 timestamp as text).
    pub kernel_loaded_at: Option<String>,

    /// Kernel tag.
    pub kernel_tag: Option<String>,

    /// Whether the kernel program is GPL compatible.
    pub kernel_gpl_compatible: Option<bool>,

    /// Kernel BTF ID.
    pub kernel_btf_id: Option<i32>,

    /// Size (in bytes) of the translated program.
    pub kernel_bytes_xlated: Option<i32>,

    /// Whether the program was JIT compiled.
    pub kernel_jited: Option<bool>,

    /// Size (in bytes) of the JIT compiled program.
    pub kernel_bytes_jited: Option<i32>,

    /// Number of verified instructions.
    pub kernel_verified_insns: Option<i32>,

    /// Kernel map IDs as a JSON array string, defaults to [].
    pub kernel_map_ids: String,

    /// Kernel allocated memory (in bytes).
    pub kernel_bytes_memlock: Option<i32>,

    /// Timestamp when the record was created.
    pub created_at: NaiveDateTime,

    /// Timestamp when the record was last updated.
    pub updated_at: NaiveDateTime,
}

#[derive(Debug, AsChangeset, Insertable, Identifiable, Selectable, Queryable)]
#[diesel(belongs_to(BpfProgram, foreign_key = program_id))]
#[diesel(table_name = crate::schema::bpf_links)]
pub struct BpfLink {
    pub id: i64, // PRIMARY KEY
    pub program_id: i64,
    pub link_type: Option<String>,
    pub target: Option<String>,
    pub state: String,
    created_at: NaiveDateTime,
    updated_at: NaiveDateTime,
}

#[derive(Debug, AsChangeset, Insertable, Identifiable, Selectable, Queryable)]
#[diesel(table_name = crate::schema::bpf_maps)]
pub struct BpfMap {
    pub id: i64, // PRIMARY KEY for Identifiable
    pub name: String,
    pub map_type: Option<String>,
    pub key_size: Option<i32>,
    pub value_size: Option<i32>,
    pub max_entries: Option<i32>,
    created_at: NaiveDateTime,
    updated_at: NaiveDateTime,
}

#[derive(Debug, Queryable, Selectable, Associations)]
#[diesel(belongs_to(BpfProgram, foreign_key = program_id))]
#[diesel(belongs_to(BpfMap, foreign_key = map_id))]
#[diesel(table_name = crate::schema::bpf_program_maps)]
pub struct BpfProgramMap {
    pub program_id: i64,
    pub map_id: i64,
}

/// BPF Program database operations.
///
/// This implementation provides a thin convenience layer over the
/// underlying database operations, isolating direct calls to Diesel.
/// The functions are intentionally simple, handling only basic CRUD
/// (Create, Read, Update, Delete) operations.
///
/// # Transaction Handling
///
/// These functions do not manage database transactions. Transaction
/// control should be handled at a higher level where operation
/// grouping and rollback behavior can be determined by business
/// logic.
///
/// # Error Handling
///
/// All functions return `QueryResult<T>`, propagating any database
/// errors to the caller for handling.
impl BpfProgram {
    /// Creates a new BPF program record in the database.
    ///
    /// Updates created_at and updated_at timestamps before insertion.
    pub fn create_record(
        conn: &mut SqliteConnection,
        program: &mut BpfProgram,
    ) -> QueryResult<BpfProgram> {
        use crate::schema::bpf_programs::dsl::*;

        program.created_at = Utc::now().naive_utc();
        program.updated_at = program.created_at;

        diesel::insert_into(crate::schema::bpf_programs::table)
            .values(&*program)
            .returning(bpf_programs::all_columns())
            .get_result(conn)
    }

    /// Returns all BPF programs in the database.
    pub fn find_all(conn: &mut SqliteConnection) -> QueryResult<Vec<BpfProgram>> {
        use crate::schema::bpf_programs::dsl::*;
        bpf_programs.load(conn)
    }

    /// Finds a BPF program by its ID.
    pub fn find_record(conn: &mut SqliteConnection, search_id: i64) -> QueryResult<BpfProgram> {
        use crate::schema::bpf_programs::dsl::*;
        bpf_programs.filter(id.eq(search_id)).first(conn)
    }

    /// Updates an existing BPF program record. Updates the updated_at
    /// timestamp. Returns the updated record if successful.
    pub fn update_record(&mut self, conn: &mut SqliteConnection) -> QueryResult<BpfProgram> {
        use crate::schema::bpf_programs::dsl::*;

        // XXX Rely on SQLite trigger?
        self.updated_at = Utc::now().naive_utc();

        diesel::update(bpf_programs.filter(id.eq(self.id)))
            .set(&*self)
            .get_result(conn)
    }

    /// Deletes a BPF program by its ID. Returns true if a record was
    /// deleted, false if no record matched the ID.
    pub fn delete_record(conn: &mut SqliteConnection, delete_id: i64) -> QueryResult<bool> {
        use crate::schema::bpf_programs::dsl::*;

        let num_deleted = diesel::delete(bpf_programs.filter(id.eq(delete_id))).execute(conn)?;

        Ok(num_deleted > 0)
    }
}

impl BpfMap {
    pub fn insert(conn: &mut SqliteConnection, mut map: BpfMap) -> QueryResult<BpfMap> {
        use crate::schema::bpf_maps::dsl::*;

        map.created_at = Utc::now().naive_utc();
        map.updated_at = map.created_at;

        diesel::insert_into(crate::schema::bpf_maps::table)
            .values(&map)
            .returning(bpf_maps::all_columns())
            .get_result(conn)
    }
}

impl BpfLink {
    pub fn link_insert(conn: &mut SqliteConnection, link: &mut BpfLink) -> QueryResult<BpfLink> {
        use crate::schema::bpf_links::dsl::*;

        link.created_at = Utc::now().naive_utc();
        link.updated_at = link.created_at;

        diesel::insert_into(crate::schema::bpf_links::table)
            .values(&*link)
            .returning(bpf_links::all_columns())
            .get_result(conn)
    }
}

impl Default for BpfProgram {
    fn default() -> Self {
        Self {
            id: 0,
            name: "".to_string(),
            description: None,
            kind: "".to_string(),
            state: "".to_string(),
            location_type: "".to_string(),
            file_path: None,
            image_url: None,
            image_pull_policy: None,
            username: None,
            password: None,
            map_pin_path: "".to_string(),
            map_owner_id: None,
            program_bytes: vec![],
            metadata: "{}".to_string(),
            global_data: "{}".to_string(),
            retprobe: None,
            fn_name: None,
            kernel_name: None,
            kernel_program_type: None,
            kernel_loaded_at: None,
            kernel_tag: None,
            kernel_gpl_compatible: None,
            kernel_btf_id: None,
            kernel_bytes_xlated: None,
            kernel_jited: None,
            kernel_bytes_jited: None,
            kernel_verified_insns: None,
            kernel_map_ids: "[]".to_string(),
            kernel_bytes_memlock: None,
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}

impl Default for BpfLink {
    fn default() -> Self {
        Self {
            id: 0, // Indicates an unsaved record
            program_id: 0,
            link_type: None,
            target: None,
            state: "".to_string(),
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}

impl Default for BpfMap {
    fn default() -> Self {
        Self {
            id: 0, // Indicates an unsaved record
            name: "".to_string(),
            map_type: None,
            key_size: None,
            value_size: None,
            max_entries: None,
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{establish_sqlite_connection, models::BpfProgram};

    fn setup_test_db() -> SqliteConnection {
        let database_url = ":memory:";
        establish_sqlite_connection(database_url)
            .expect("Failed to establish in-memory SQLite connection")
    }

    #[test]
    /// Tests the insertion and retrieval of BPF programs in the
    /// database.
    ///
    /// This test verifies several aspects in sequence:
    ///
    /// 1. Program Creation:
    ///    - Creates a minimal but valid BPF program with required fields
    ///    - Verifies default timestamps are set to epoch
    ///
    /// 2. Program Insertion:
    ///    - Tests successful insertion into the database
    ///    - Verifies timestamps are updated (no longer epoch)
    ///    - After syncing timestamps, confirms complete equality between
    ///      input and inserted program
    ///
    /// 3. Default Values and JSON Validity:
    ///    - Confirms metadata defaults to "{}"
    ///    - Confirms global_data defaults to "{}"
    ///    - Confirms kernel_map_ids defaults to "[]"
    ///    - Verifies all are valid JSON structures
    ///
    /// 4. Record Retrieval:
    ///    - Tests the find_record operation by ID
    ///    - Verifies complete equality between inserted and retrieved records
    ///
    /// The test uses Eq for complete record comparison after
    /// synchronising timestamps, providing thorough verification of
    /// all fields through the database round-trip.
    fn test_insert_and_find_bpf_program() {
        let mut db_conn = setup_test_db();

        // Setup test program with minimal required fields.
        let mut prog = BpfProgram::default();
        prog.id = 100;
        prog.name = "xdp_test_program".to_string();
        prog.kind = "xdp".to_string();
        prog.state = "pre_load".to_string();
        prog.location_type = "file".to_string();
        prog.file_path = Some("/path/to/test_program.o".to_string());
        prog.map_pin_path = "/sys/fs/bpf/test_program".to_string();
        prog.program_bytes = vec![0xAA, 0xBB, 0xCC];

        // Verify default timestamps are epoch.
        let epoch: NaiveDateTime = Default::default();
        assert_eq!(prog.created_at, epoch, "Default created_at should be epoch");
        assert_eq!(prog.updated_at, epoch, "Default updated_at should be epoch");

        // Insert program and verify.
        let inserted_program =
            BpfProgram::create_record(&mut db_conn, &mut prog).expect("Insert failed");

        // Verify timestamps are no longer epoch
        assert_ne!(
            inserted_program.created_at, epoch,
            "created_at should not be epoch after insert"
        );
        assert_ne!(
            inserted_program.updated_at, epoch,
            "updated_at should not be epoch after insert"
        );

        // Sync timestamps to enable Eq comparisons.
        prog.created_at = inserted_program.created_at;
        prog.updated_at = inserted_program.updated_at;

        assert_eq!(prog, inserted_program);

        // Verify JSON field defaults and validity.
        {
            assert_eq!(inserted_program.metadata, "{}");
            assert_eq!(inserted_program.global_data, "{}");
            assert_eq!(inserted_program.kernel_map_ids, "[]");

            serde_json::from_str::<serde_json::Value>(&inserted_program.metadata)
                .expect("metadata should be valid JSON");
            serde_json::from_str::<serde_json::Value>(&inserted_program.global_data)
                .expect("global_data should be valid JSON");
            let map_ids = serde_json::from_str::<Vec<i64>>(&inserted_program.kernel_map_ids)
                .expect("kernel_map_ids should be a valid JSON array of integers");
            assert!(map_ids.is_empty());
        }

        // Verify record retrieval using full Eq comparison.
        {
            let found_program =
                BpfProgram::find_record(&mut db_conn, prog.id).expect("Failed to find program");
            assert_eq!(found_program, inserted_program);
        }
    }

    #[test]
    /// This test verifies the serialization, deserialization, and
    /// database persistence of BpfProgram structs. It performs two
    /// round-trip tests:
    ///
    /// 1. JSON round-trip:
    ///    - Creates a BpfProgram with all fields populated
    ///    - Serializes it to JSON
    ///    - Deserializes back to a BpfProgram
    ///    - Verifies all fields match the original
    //
    ///    This ensures the Serde derive macros work correctly for all
    ///    field types (i64, String, Option<String>, Vec<u8>, etc.)
    ///    and that no data is lost in the JSON conversion.
    ///
    /// 2. Database round-trip:
    ///    - Takes the same BpfProgram
    ///    - Inserts it into SQLite
    ///    - Retrieves it
    ///    - Serializes to JSON
    ///    - Deserializes back to a BpfProgram
    ///    - Verifies all fields match
    //
    ///    This ensures:
    ///    - Diesel's type mappings work correctly for all fields.
    ///    - The database schema matches the struct.
    ///    - No data is lost or corrupted during database operations.
    ///    - Timestamps are handled correctly.
    ///    - Optional fields are preserved.
    ///    - Binary data is stored and retrieved accurately.
    ///    - JSON string fields (metadata, global_data,
    ///      kernel_map_ids) maintain their format.
    fn test_bpf_program_serde_roundtrip() {
        // Create program with all fields populated.
        let mut prog = BpfProgram::default();
        prog.id = 100;
        prog.name = "xdp_test_program".to_string();
        prog.description = Some("Test program description".to_string());
        prog.kind = "xdp".to_string();
        prog.state = "pre_load".to_string();
        prog.location_type = "file".to_string();
        prog.file_path = Some("/path/to/test_program.o".to_string());
        prog.image_url = Some("registry.example.com/image:tag".to_string());
        prog.image_pull_policy = Some("Always".to_string());
        prog.username = Some("testuser".to_string());
        prog.password = Some("testpass".to_string());
        prog.map_pin_path = "/sys/fs/bpf/test_program".to_string();
        prog.map_owner_id = Some(1234);
        prog.program_bytes = vec![0xAA, 0xBB, 0xCC];
        prog.metadata = "{}".to_string();
        prog.global_data = "{}".to_string();
        prog.retprobe = Some(true);
        prog.fn_name = Some("test_function".to_string());
        prog.kernel_name = Some("test_kernel_prog".to_string());
        prog.kernel_program_type = Some(123);
        prog.kernel_loaded_at = Some("2024-02-18T12:00:00Z".to_string());
        prog.kernel_tag = Some("abcdef123456".to_string());
        prog.kernel_gpl_compatible = Some(true);
        prog.kernel_btf_id = Some(456);
        prog.kernel_bytes_xlated = Some(1024);
        prog.kernel_jited = Some(true);
        prog.kernel_bytes_jited = Some(2048);
        prog.kernel_verified_insns = Some(100);
        prog.kernel_map_ids = "[]".to_string();
        prog.kernel_bytes_memlock = Some(4096);

        // Test JSON serialization round-trip.
        {
            let json = serde_json::to_string(&prog).expect("Failed to serialize to JSON");

            let deserialized: BpfProgram =
                serde_json::from_str(&json).expect("Failed to deserialize from JSON");

            assert_eq!(prog, deserialized);
        }

        // Test database round-trip.
        {
            let mut db_conn = setup_test_db();

            let inserted =
                BpfProgram::create_record(&mut db_conn, &mut prog).expect("Failed to insert");

            let json_after_db =
                serde_json::to_string(&inserted).expect("Failed to serialize after DB");

            let deserialized_after_db: BpfProgram =
                serde_json::from_str(&json_after_db).expect("Failed to deserialize after DB");

            assert_eq!(inserted, deserialized_after_db);
        }
    }
}
