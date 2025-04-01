// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use chrono::NaiveDateTime;
use diesel::prelude::*;

use crate::db::{KernelU32, U64Blob};

#[derive(
    Clone,
    Debug,
    Eq,
    PartialEq,
    serde::Serialize,
    serde::Deserialize,
    AsChangeset,
    Insertable,
    Identifiable,
    Queryable,
)]
#[diesel(table_name = crate::db::bpf_programs)]
#[diesel(primary_key(id))]
pub struct BpfProgram {
    pub id: KernelU32,
    pub name: String,
    pub kind: String,
    pub state: String,
    pub location_type: String,
    pub file_path: Option<String>,
    pub image_url: Option<String>,
    pub image_pull_policy: Option<String>,
    pub username: Option<String>,
    pub password: Option<String>,
    pub map_pin_path: String,
    pub map_owner_id: Option<KernelU32>,
    #[diesel(sql_type = diesel::sql_types::Binary)]
    #[serde(skip)]
    pub program_bytes: Vec<u8>,
    pub metadata: Option<String>,
    pub global_data: Option<String>,
    pub retprobe: Option<bool>,
    pub fn_name: Option<String>,
    pub kernel_name: Option<String>,
    pub kernel_program_type: Option<KernelU32>,
    pub kernel_loaded_at: Option<String>,
    pub kernel_tag: U64Blob,
    pub kernel_gpl_compatible: Option<bool>,
    pub kernel_btf_id: Option<KernelU32>,
    pub kernel_bytes_xlated: Option<KernelU32>,
    pub kernel_jited: Option<bool>,
    pub kernel_bytes_jited: Option<KernelU32>,
    pub kernel_verified_insns: Option<KernelU32>,
    pub kernel_bytes_memlock: Option<KernelU32>,
    pub created_at: NaiveDateTime,
    pub updated_at: Option<NaiveDateTime>,
}

impl BpfProgram {
    pub fn insert_record(
        conn: &mut SqliteConnection,
        program: &BpfProgram,
    ) -> QueryResult<BpfProgram> {
        diesel::insert_into(crate::db::bpf_programs::table)
            .values(program)
            .returning(crate::db::bpf_programs::all_columns)
            .get_result(conn)
    }

    pub fn find_all(conn: &mut SqliteConnection) -> QueryResult<Vec<BpfProgram>> {
        use crate::db::bpf_programs::dsl::*;
        bpf_programs.load(conn)
    }

    pub fn find_record(
        conn: &mut SqliteConnection,
        search_id: KernelU32,
    ) -> QueryResult<BpfProgram> {
        use crate::db::bpf_programs::dsl::*;
        bpf_programs.filter(id.eq(search_id)).first(conn)
    }

    pub fn update_record(&mut self, conn: &mut SqliteConnection) -> QueryResult<()> {
        use crate::db::bpf_programs::dsl::*;
        let updated: BpfProgram = diesel::update(bpf_programs.filter(id.eq(self.id)))
            .set(&*self)
            .get_result(conn)?;
        *self = updated;
        Ok(())
    }

    pub fn delete_record(conn: &mut SqliteConnection, delete_id: KernelU32) -> QueryResult<bool> {
        use crate::db::bpf_programs::dsl::*;
        let num_deleted = diesel::delete(bpf_programs.filter(id.eq(delete_id))).execute(conn)?;
        Ok(num_deleted > 0)
    }
}

#[cfg(test)]
impl Default for BpfProgram {
    fn default() -> Self {
        Self {
            id: 0u32.into(),
            name: "".to_owned(),
            kind: "".to_owned(),
            state: "".to_owned(),
            location_type: "".to_owned(),
            file_path: None,
            image_url: None,
            image_pull_policy: None,
            username: None,
            password: None,
            map_pin_path: "".to_owned(),
            map_owner_id: None,
            program_bytes: vec![],
            metadata: None,
            global_data: None,
            retprobe: None,
            fn_name: None,
            kernel_name: None,
            kernel_program_type: None,
            kernel_loaded_at: None,
            kernel_tag: U64Blob::from(0u64),
            kernel_gpl_compatible: None,
            kernel_btf_id: None,
            kernel_bytes_xlated: None,
            kernel_jited: None,
            kernel_bytes_jited: None,
            kernel_verified_insns: None,
            kernel_bytes_memlock: None,
            created_at: Default::default(),
            updated_at: Default::default(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{BpfProgramMap, establish_database_connection};

    fn setup_test_db() -> SqliteConnection {
        let database_url = ":memory:";
        establish_database_connection(database_url)
            .expect("Failed to establish in-memory SQLite connection")
    }

    /// Verifies that the SQLite PRAGMA foreign_keys setting is
    /// enabled (value = 1) when establishing database connections
    /// through our standard method.
    ///
    /// This test is critical because:
    /// 1. SQLite does NOT enforce foreign key constraints by default.
    /// 2. Our schema depends heavily on ON DELETE CASCADE behaviors for referential integrity.
    /// 3. Without this setting enabled, cascading deletes silently fail, causing orphaned data.
    ///
    /// If this test fails, cascade operations like automatic map
    /// cleanup won't work properly. We also want to regress early
    /// should the pragma be inadvertently removed.
    #[test]
    fn test_foreign_keys_pragma_enabled() {
        let mut conn = setup_test_db();

        #[derive(QueryableByName, Debug)]
        struct ForeignKeySetting {
            #[diesel(sql_type = diesel::sql_types::Integer)]
            foreign_keys: i32,
        }

        let result = diesel::sql_query("PRAGMA foreign_keys")
            .load::<ForeignKeySetting>(&mut conn)
            .expect("Failed to query foreign_keys PRAGMA");

        let foreign_keys_enabled = result
            .first()
            .expect("Expected one row from PRAGMA foreign_keys")
            .foreign_keys;

        assert_eq!(
            foreign_keys_enabled, 1,
            "PRAGMA foreign_keys is not enabled! This will break cascade deletes and referential integrity."
        );
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
        // Ensure BpfProgram derives Clone (if not, add #[derive(Clone)] to its definition).
        let prog = BpfProgram {
            id: 100u32.into(),
            name: "xdp_test_program".to_owned(),
            kind: "xdp".to_owned(),
            state: "pre_load".to_owned(),
            location_type: "file".to_owned(),
            file_path: Some("/path/to/test_program.o".to_owned()),
            map_pin_path: "/sys/fs/bpf/test_program".to_owned(),
            program_bytes: vec![0xAA, 0xBB, 0xCC],
            ..Default::default()
        };

        // Verify default timestamps are epoch.
        let epoch: NaiveDateTime = Default::default();
        assert_eq!(prog.created_at, epoch, "Default created_at should be epoch");
        assert_eq!(prog.updated_at, None, "Default updated_at should be None");

        // Clone prog so we have a copy to compare later.
        let mut prog_for_assert = prog.clone();

        // Insert program.
        // Note: insert_record now takes ownership of `prog`.
        let inserted_program =
            BpfProgram::insert_record(&mut db_conn, &prog).expect("Insert failed");

        // Sync timestamps to enable Eq comparisons.
        prog_for_assert.created_at = inserted_program.created_at;
        prog_for_assert.updated_at = inserted_program.updated_at;

        // Assert that the modified copy equals the inserted record.
        assert_eq!(prog_for_assert, inserted_program);

        // Verify record retrieval using full Eq comparison.
        {
            let found_program = BpfProgram::find_record(&mut db_conn, prog_for_assert.id)
                .expect("Failed to find program");
            assert_eq!(found_program, inserted_program);
        }
    }

    #[test]
    /// This test verifies the serialisation, deserialisation, and
    /// database persistence of BpfProgram structs. It ensures that:
    ///
    /// - The Serde derive macros work correctly for all field types
    ///   (KernelU32, String, Option<String>, Vec<u8>, etc.).
    /// - No data is lost in the JSON conversion.
    /// - Diesel's type mappings are correct for all fields.
    /// - The database schema matches the struct.
    /// - No data is lost or corrupted during database operations.
    /// - Timestamps are handled correctly.
    /// - Optional fields are preserved.
    /// - Binary data is stored and retrieved accurately.
    /// - JSON string fields (metadata, global_data, kernel_map_ids)
    ///   maintain their format.
    ///
    /// It performs two round-trip tests:
    ///
    /// 1. **JSON round-trip:**
    ///    - Creates a BpfProgram with all fields populated.
    ///    - Serialises it to JSON.
    ///    - Deserialises back to a BpfProgram.
    ///    - Verifies all fields match the original.
    ///
    /// 2. **Database round-trip:**
    ///    - Takes the same BpfProgram.
    ///    - Inserts it into SQLite.
    ///    - Retrieves it.
    ///    - Serialises to JSON.
    ///    - Deserialises back to a BpfProgram.
    ///    - Verifies all fields match.
    fn test_bpf_program_serde_roundtrip() {
        let prog = BpfProgram {
            id: 100u32.into(),
            name: "xdp_test_program".to_owned(),
            kind: "xdp".to_owned(),
            state: "pre_load".to_owned(),
            location_type: "file".to_owned(),
            file_path: Some("/path/to/test_program.o".to_owned()),
            image_url: Some("registry.example.com/image:tag".to_owned()),
            image_pull_policy: Some("Always".to_owned()),
            username: Some("testuser".to_owned()),
            password: Some("testpass".to_owned()),
            map_pin_path: "/sys/fs/bpf/test_program".to_owned(),
            map_owner_id: Some(1234u32.into()),
            program_bytes: vec![0xAA, 0xBB, 0xCC],
            metadata: Some("{}".to_owned()),
            global_data: Some("{}".to_owned()),
            retprobe: Some(true),
            fn_name: Some("test_function".to_owned()),
            kernel_name: Some("test_kernel_prog".to_owned()),
            kernel_program_type: Some(123u32.into()),
            kernel_loaded_at: Some("2024-02-18T12:00:00Z".to_owned()),
            kernel_tag: U64Blob::from(u64::MAX),
            kernel_gpl_compatible: Some(true),
            kernel_btf_id: Some(456u32.into()),
            kernel_bytes_xlated: Some(1024u32.into()),
            kernel_jited: Some(true),
            kernel_bytes_jited: Some(2048u32.into()),
            kernel_verified_insns: Some(100u32.into()),
            kernel_bytes_memlock: Some(4096u32.into()),
            ..Default::default()
        };

        // Test JSON serialisation round-trip.
        {
            let json = serde_json::to_string(&prog).expect("Failed to serialize to JSON");

            let mut deserialized: BpfProgram =
                serde_json::from_str(&json).expect("Failed to deserialize from JSON");

            // Manually restore the skipped field for comparison.
            deserialized.program_bytes = prog.program_bytes.clone();

            assert_eq!(prog, deserialized);
        }

        // Test database round-trip.
        {
            let mut db_conn = setup_test_db();

            let inserted =
                BpfProgram::insert_record(&mut db_conn, &prog).expect("Failed to insert");

            let json_after_db =
                serde_json::to_string(&inserted).expect("Failed to serialize after DB");

            let mut deserialized_after_db: BpfProgram =
                serde_json::from_str(&json_after_db).expect("Failed to deserialize after DB");

            // Manually restore the skipped field for comparison.
            deserialized_after_db.program_bytes = prog.program_bytes.clone();

            assert_eq!(inserted, deserialized_after_db);
        }
    }

    #[test]
    /// Verifies cascade + trigger behaviour across multiple programs
    /// sharing the same BPF map. Ensures the map is only deleted once
    /// all referencing programs are removed.
    ///
    /// Test plan:
    ///
    /// 1. Insert two BPF programs: prog1 and prog2.
    /// 2. Insert one shared map.
    /// 3. Link both programs to the map.
    /// 4. Confirm both program_map links exist.
    /// 5. Delete prog1 — program_map row is deleted, map remains.
    /// 6. Confirm only prog2's mapping remains.
    /// 7. Delete prog2 — remaining mapping and map are deleted.
    /// 8. Confirm bpf_maps is now empty.
    fn test_program_map_cascade_deletes_map_only_when_unused() {
        use crate::{
            BpfMap,
            db::{
                bpf_maps::dsl::bpf_maps,
                bpf_program_maps::dsl::{
                    bpf_program_maps, map_id as map_id_col, program_id as program_id_col,
                },
            },
        };

        let mut conn = setup_test_db();

        // Shared map inserted once.
        let shared_map = BpfMap {
            id: 900u32.into(),
            name: "shared_map".to_owned(),
            map_type: Some("Array".to_owned()),
            key_size: 4u32.into(),
            value_size: 64u32.into(),
            max_entries: 128u32.into(),
            created_at: Default::default(),
            updated_at: Default::default(),
        };
        BpfMap::insert_record_on_conflict_do_nothing(&mut conn, &shared_map).unwrap();

        // Insert first program.
        let prog1 = BpfProgram {
            id: 101u32.into(),
            name: "prog1".into(),
            kind: "tracepoint".into(),
            state: "pre_load".into(),
            location_type: "file".into(),
            file_path: Some("/tmp/prog1.o".into()),
            map_pin_path: "/sys/fs/bpf/prog1".into(),
            program_bytes: vec![0x1],
            metadata: Some("{}".into()),
            global_data: Some("{}".into()),
            ..Default::default()
        };
        BpfProgram::insert_record(&mut conn, &prog1).unwrap();

        let prog2 = BpfProgram {
            id: 102u32.into(),
            name: "prog2".into(),
            kind: "tracepoint".into(),
            state: "pre_load".into(),
            location_type: "file".into(),
            file_path: Some("/tmp/prog2.o".into()),
            map_pin_path: "/sys/fs/bpf/prog2".into(),
            program_bytes: vec![0x2],
            metadata: Some("{}".into()),
            global_data: Some("{}".into()),
            ..Default::default()
        };
        BpfProgram::insert_record(&mut conn, &prog2).unwrap();

        // Link both programs to the shared map.
        BpfProgramMap::insert_record(&mut conn, prog1.id, shared_map.id).unwrap();
        BpfProgramMap::insert_record(&mut conn, prog2.id, shared_map.id).unwrap();

        // Confirm both join rows exist.
        let mappings: Vec<(KernelU32, KernelU32)> = bpf_program_maps
            .select((program_id_col, map_id_col))
            .order_by(program_id_col)
            .load(&mut conn)
            .unwrap();
        assert_eq!(
            mappings,
            vec![(prog1.id, shared_map.id), (prog2.id, shared_map.id)],
            "Expected both program_map rows to exist"
        );

        // Delete first program.
        BpfProgram::delete_record(&mut conn, prog1.id).unwrap();

        let mappings: Vec<(KernelU32, KernelU32)> = bpf_program_maps
            .select((program_id_col, map_id_col))
            .load(&mut conn)
            .unwrap();
        assert_eq!(
            mappings,
            vec![(prog2.id, shared_map.id)],
            "Expected only prog2 mapping to remain"
        );

        // Confirm map still exists.
        let maps: Vec<BpfMap> = bpf_maps.load(&mut conn).unwrap();
        assert_eq!(maps.len(), 1, "Expected shared map to still exist");

        BpfProgram::delete_record(&mut conn, prog2.id).unwrap();

        let mappings: Vec<(KernelU32, KernelU32)> = bpf_program_maps
            .select((program_id_col, map_id_col))
            .load(&mut conn)
            .unwrap();
        assert!(
            mappings.is_empty(),
            "Expected all program_map rows to be gone"
        );

        // Confirm shared map is now deleted.
        let maps: Vec<BpfMap> = bpf_maps.load(&mut conn).unwrap();
        assert!(
            maps.is_empty(),
            "Expected shared map to be deleted after all program references removed"
        );
    }

    #[cfg(test)]
    mod bpfprogram_constraint_tests {
        use diesel::result::{DatabaseErrorKind, Error};

        use super::*;

        fn create_valid_base_program() -> BpfProgram {
            BpfProgram {
                id: 100u32.into(),
                name: "test_program".to_owned(),
                kind: "xdp".to_owned(),
                state: "pre_load".to_owned(),
                location_type: "file".to_owned(),
                file_path: Some("/path/to/test_program.o".to_owned()),
                map_pin_path: "/sys/fs/bpf/test_program".to_owned(),
                program_bytes: vec![0xAA, 0xBB, 0xCC],
                ..Default::default()
            }
        }

        fn assert_constraint_violation(result: Result<BpfProgram, Error>) {
            match result {
                Err(Error::DatabaseError(DatabaseErrorKind::CheckViolation, _)) => {
                    // This is the expected outcome for a constraint
                    // violation.
                }
                Err(e) => panic!(
                    "Expected check constraint violation, got different error: {:?}",
                    e
                ),
                Ok(_) => {
                    panic!("Expected insertion to fail with constraint violation, but it succeeded")
                }
            }
        }

        #[test]
        /// Tests that the 'kind' field constraint is enforced. The
        /// schema only allows specific values: 'xdp', 'tc', 'tcx',
        /// 'tracepoint', 'kprobe', 'uprobe', 'fentry', 'fexit'.
        fn test_kind_constraint() {
            let mut conn = setup_test_db();

            // Valid kind should succeed.
            let valid_program = create_valid_base_program();
            BpfProgram::insert_record(&mut conn, &valid_program)
                .expect("Valid program should insert successfully");

            // Invalid kind should fail.
            let mut invalid_program = create_valid_base_program();
            invalid_program.id = 101u32.into();
            invalid_program.kind = "invalid_kind".to_owned();

            let result = BpfProgram::insert_record(&mut conn, &invalid_program);
            assert_constraint_violation(result);
        }

        #[test]
        /// Tests that the 'state' field constraint is enforced. The
        /// schema only allows 'pre_load' or 'loaded'.
        fn test_state_constraint() {
            let mut conn = setup_test_db();

            // Valid state should succeed.
            let valid_program = create_valid_base_program();
            BpfProgram::insert_record(&mut conn, &valid_program)
                .expect("Valid program should insert successfully");

            // Test with 'loaded' state, which should also be valid.
            let mut loaded_program = create_valid_base_program();
            loaded_program.id = 101u32.into();
            loaded_program.state = "loaded".to_owned();
            BpfProgram::insert_record(&mut conn, &loaded_program)
                .expect("Program with 'loaded' state should insert successfully");

            // Invalid state should fail.
            let mut invalid_program = create_valid_base_program();
            invalid_program.id = 102u32.into();
            invalid_program.state = "running".to_owned();

            let result = BpfProgram::insert_record(&mut conn, &invalid_program);
            assert_constraint_violation(result);
        }

        #[test]
        /// Tests that the 'location_type' and corresponding field
        /// constraints are enforced. If location_type is 'file',
        /// file_path must be provided. If location_type is 'image',
        /// image_url must be provided.
        fn test_location_type_constraints() {
            let mut conn = setup_test_db();

            // Valid file location should succeed.
            let valid_file_program = create_valid_base_program();
            BpfProgram::insert_record(&mut conn, &valid_file_program)
                .expect("Valid file-based program should insert successfully");

            // Valid image location should succeed.
            let mut valid_image_program = create_valid_base_program();
            valid_image_program.id = 101u32.into();
            valid_image_program.location_type = "image".to_owned();
            valid_image_program.file_path = None;
            valid_image_program.image_url = Some("registry.example.com/image:tag".to_owned());
            BpfProgram::insert_record(&mut conn, &valid_image_program)
                .expect("Valid image-based program should insert successfully");

            // Invalid: File location type without file_path.
            let mut invalid_file_program = create_valid_base_program();
            invalid_file_program.id = 102u32.into();
            invalid_file_program.file_path = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_file_program);
            assert_constraint_violation(result);

            // Invalid: Image location type without image_url.
            let mut invalid_image_program = create_valid_base_program();
            invalid_image_program.id = 103u32.into();
            invalid_image_program.location_type = "image".to_owned();
            invalid_image_program.file_path = None;
            invalid_image_program.image_url = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_image_program);
            assert_constraint_violation(result);

            // Invalid location_type should fail.
            let mut invalid_location_program = create_valid_base_program();
            invalid_location_program.id = 104u32.into();
            invalid_location_program.location_type = "network".to_owned();

            let result = BpfProgram::insert_record(&mut conn, &invalid_location_program);
            assert_constraint_violation(result);
        }

        #[test]
        /// Tests that the function name constraint is enforced for
        /// 'fentry' and 'fexit' program types.
        fn test_function_name_constraint() {
            let mut conn = setup_test_db();

            // Valid fentry program with fn_name should succeed.
            let mut valid_fentry_program = create_valid_base_program();
            valid_fentry_program.kind = "fentry".to_owned();
            valid_fentry_program.fn_name = Some("test_function".to_owned());

            BpfProgram::insert_record(&mut conn, &valid_fentry_program)
                .expect("Valid fentry program should insert successfully");

            // Valid fexit program with fn_name should succeed.
            let mut valid_fexit_program = create_valid_base_program();
            valid_fexit_program.id = 101u32.into();
            valid_fexit_program.kind = "fexit".to_owned();
            valid_fexit_program.fn_name = Some("test_function".to_owned());

            BpfProgram::insert_record(&mut conn, &valid_fexit_program)
                .expect("Valid fexit program should insert successfully");

            // Invalid: fentry program without fn_name should fail.
            let mut invalid_fentry_program = create_valid_base_program();
            invalid_fentry_program.id = 102u32.into();
            invalid_fentry_program.kind = "fentry".to_owned();
            invalid_fentry_program.fn_name = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_fentry_program);
            assert_constraint_violation(result);

            // Invalid: fexit program without fn_name should fail.
            let mut invalid_fexit_program = create_valid_base_program();
            invalid_fexit_program.id = 103u32.into();
            invalid_fexit_program.kind = "fexit".to_owned();
            invalid_fexit_program.fn_name = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_fexit_program);
            assert_constraint_violation(result);

            // Other program types don't require fn_name.
            let mut tracepoint_program = create_valid_base_program();
            tracepoint_program.id = 104u32.into();
            tracepoint_program.kind = "tracepoint".to_owned();
            tracepoint_program.fn_name = None;

            BpfProgram::insert_record(&mut conn, &tracepoint_program)
                .expect("Tracepoint program without fn_name should insert successfully");
        }

        #[test]
        /// Tests that the retprobe constraint is enforced for
        /// 'kprobe' and 'uprobe' program types.
        fn test_retprobe_constraint() {
            let mut conn = setup_test_db();

            // Valid kprobe program with retprobe should succeed.
            let mut valid_kprobe_program = create_valid_base_program();
            valid_kprobe_program.kind = "kprobe".to_owned();
            valid_kprobe_program.retprobe = Some(false);

            BpfProgram::insert_record(&mut conn, &valid_kprobe_program)
                .expect("Valid kprobe program should insert successfully");

            // Valid uprobe program with retprobe should succeed.
            let mut valid_uprobe_program = create_valid_base_program();
            valid_uprobe_program.id = 101u32.into();
            valid_uprobe_program.kind = "uprobe".to_owned();
            valid_uprobe_program.retprobe = Some(true);

            BpfProgram::insert_record(&mut conn, &valid_uprobe_program)
                .expect("Valid uprobe program should insert successfully");

            // Invalid: kprobe program without retprobe should fail.
            let mut invalid_kprobe_program = create_valid_base_program();
            invalid_kprobe_program.id = 102u32.into();
            invalid_kprobe_program.kind = "kprobe".to_owned();
            invalid_kprobe_program.retprobe = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_kprobe_program);
            assert_constraint_violation(result);

            // Invalid: uprobe program without retprobe should fail.
            let mut invalid_uprobe_program = create_valid_base_program();
            invalid_uprobe_program.id = 103u32.into();
            invalid_uprobe_program.kind = "uprobe".to_owned();
            invalid_uprobe_program.retprobe = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_uprobe_program);
            assert_constraint_violation(result);

            // Other program types don't require retprobe.
            let mut xdp_program = create_valid_base_program();
            xdp_program.id = 104u32.into();
            xdp_program.kind = "xdp".to_owned();
            xdp_program.retprobe = None;

            BpfProgram::insert_record(&mut conn, &xdp_program)
                .expect("XDP program without retprobe should insert successfully");
        }

        #[test]
        /// Test that program can be updated while still enforcing
        /// constraints.
        fn test_update_with_constraints() {
            let mut conn = setup_test_db();

            // Create a valid program.
            let program = create_valid_base_program();
            let inserted_program = BpfProgram::insert_record(&mut conn, &program)
                .expect("Valid program should insert successfully");

            // Valid update should succeed.
            let mut program_to_update = inserted_program.clone();
            program_to_update.name = "updated_name".to_owned();
            program_to_update
                .update_record(&mut conn)
                .expect("Valid update should succeed");

            // Invalid update should fail (invalid kind).
            let mut invalid_update = inserted_program.clone();
            invalid_update.kind = "invalid_kind".to_owned();
            let result = invalid_update.update_record(&mut conn);
            match result {
                Err(Error::DatabaseError(DatabaseErrorKind::CheckViolation, _)) => {
                    // This is the expected outcome
                }
                Err(e) => panic!(
                    "Expected check constraint violation, got different error: {:?}",
                    e
                ),
                Ok(_) => {
                    panic!("Expected update to fail with constraint violation, but it succeeded")
                }
            }

            // Invalid update should fail (missing required field
            // based on kind).
            let mut invalid_update = inserted_program.clone();
            invalid_update.kind = "fentry".to_owned();
            invalid_update.fn_name = None;
            let result = invalid_update.update_record(&mut conn);
            assert_constraint_violation(Result::Err(result.unwrap_err()));
        }

        #[test]
        /// Test combination of multiple constraints.
        fn test_combined_constraints() {
            let mut conn = setup_test_db();

            // Complex valid case: fentry program with image location.
            let mut program = create_valid_base_program();
            program.id = 101u32.into();
            program.kind = "fentry".to_owned();
            program.fn_name = Some("test_function".to_owned());
            program.location_type = "image".to_owned();
            program.file_path = None;
            program.image_url = Some("registry.example.com/image:tag".to_owned());
            program.image_pull_policy = Some("Always".to_owned());

            BpfProgram::insert_record(&mut conn, &program)
                .expect("Valid complex program should insert successfully");

            // Invalid complex case: missing multiple required fields.
            let mut invalid_complex_program = create_valid_base_program();
            invalid_complex_program.id = 102u32.into();
            invalid_complex_program.kind = "kprobe".to_owned(); // Requires retprobe
            invalid_complex_program.retprobe = None;
            invalid_complex_program.location_type = "image".to_owned(); // Requires image_url
            invalid_complex_program.file_path = None;
            invalid_complex_program.image_url = None;

            let result = BpfProgram::insert_record(&mut conn, &invalid_complex_program);
            assert_constraint_violation(result);
        }
    }
}
