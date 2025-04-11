// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Provides wrapper types ([`U8Blob`], [`U16Blob`], [`U32Blob`],
//! [`U64Blob`], [`U128Blob`]) for storing Rust unsigned integers in
//! SQLite BLOB columns. SQLite’s native INTEGER is signed (and capped
//! at 64 bits), so to persist a Rust `u64` (or wider) you declare
//! your schema column as BLOB but use `U64Blob` (or the appropriate
//! width) in your Rust struct.
//!
//! On **write**, the wrapper converts the primitive into a
//! fixed-length, big-endian byte array. On **read**, it converts back
//! into the primitive, failing with `UIntBlobError::InvalidSize {
//! expected, actual, type_name }` if the stored BLOB’s length does
//! not exactly match the type’s byte width. This eliminates silent
//! truncation or padding errors.
//!
//! ## Diesel Integration
//!
//! These types integrate seamlessly with Diesel's SQLite backend.
//!
//! They implement [`ToSql<Binary, Sqlite>`] and [`FromSql<Binary,
//! Sqlite>`], and derive [`diesel::sql_types::Binary`] via
//! [`diesel::expression::AsExpression`] and
//! [`diesel::deserialize::FromSqlRow`].
//!
//! This allows them to be used directly in structs that derive
//! `Insertable` and `Queryable`, without requiring manual trait
//! implementations or extra annotations.
//!
//! # Example
//!
//! ```rust
//! # use diesel::prelude::*;
//! # use bpfman::{U32Blob, U16Blob, UxBlobError};
//! # table! {
//! #     counters (id) {
//! #         id -> Integer,
//! #         value -> Binary,
//! #     }
//! # }
//! # #[derive(Queryable, Insertable)]
//! # #[diesel(table_name = counters)]
//! # struct Counter32 {
//! #     id: i32,
//! #     value: U32Blob,
//! # }
//! # #[derive(Debug, Queryable)]
//! # #[diesel(table_name = counters)]
//! # struct Counter16 {
//! #     id: i32,
//! #     value: U16Blob,
//! # }
//! # fn example() -> Result<(), Box<dyn std::error::Error>> {
//! # let mut conn = SqliteConnection::establish(":memory:")?;
//! # diesel::sql_query("CREATE TABLE counters (id INTEGER PRIMARY KEY, value BLOB NOT NULL)").execute(&mut conn)?;
//!
//! // Create U32Blob values and insert them.
//! let counters = vec![
//!     Counter32 { id: 1, value: U32Blob::from(100u32) },
//!     Counter32 { id: 2, value: U32Blob::from(200u32) },
//!     Counter32 { id: 3, value: U32Blob::from(50u32) },
//! ];
//!
//! diesel::insert_into(counters::table)
//!     .values(&counters)
//!     .execute(&mut conn)?;
//!
//! // Query with ordering preserved (50, 100, 200).
//! let ordered_results = counters::table
//!     .order_by(counters::value.asc())
//!     .load::<Counter32>(&mut conn)?;
//!
//! assert_eq!(ordered_results[0].value.get(), 50u32);
//! assert_eq!(ordered_results[2].value.get(), 200u32);
//!
//! // Access the inner value using `.get()`.
//! for counter in &ordered_results {
//!     println!("id={} value={}", counter.id, counter.value.get());
//! }
//!
//! // Filter for values greater than 75.
//! let filtered_results = counters::table
//!     .filter(counters::value.gt(U32Blob::from(75u32)))
//!     .load::<Counter32>(&mut conn)?;
//!
//! assert_eq!(filtered_results.len(), 2); // 100 and 200
//!
//! // ERROR CASE: Attempting to read a U32Blob as U16Blob.
//! let result = counters::table.find(1).first::<Counter16>(&mut conn);
//!
//! // This will fail with an UxBlobError.
//! assert!(result.is_err());
//! let error = result.unwrap_err().to_string();
//! assert!(error.contains("Invalid input size"));
//! # Ok(())
//! # }
//! ```

use diesel::{
    backend::Backend,
    deserialize::{FromSql, FromSqlRow},
    expression::AsExpression,
    serialize::{IsNull, Output, ToSql},
    sql_types::Binary,
    sqlite::Sqlite,
};
use serde::{Deserialize, Serialize};

/// Error type for decoding byte slices into unsigned integer
/// wrappers.
///
/// These errors occur when validating binary input, typically read
/// from SQLite BLOB columns, during deserialisation into typed
/// wrappers like [`U32Blob`].
#[derive(Debug, Clone, PartialEq)]
pub enum UxBlobError {
    /// Error when the byte slice has an invalid size for the
    /// requested type.
    ///
    /// This error occurs when converting a byte slice into a wrapped
    /// integer type (e.g., [`U32Blob`]) and the slice length does not
    /// match the expected size.
    ///
    /// # Fields
    ///
    /// * `expected` - The number of bytes expected for the requested type
    /// * `actual` - The actual number of bytes in the provided slice
    /// * `type_name` - The name of the requested type (e.g., "u16", "u32")
    InvalidSize {
        expected: usize,
        actual: usize,
        type_name: String,
    },
}

impl std::fmt::Display for UxBlobError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidSize {
                expected,
                actual,
                type_name,
            } => {
                write!(
                    f,
                    "Invalid input size: expected {} bytes for `{}`, got {}",
                    expected, type_name, actual
                )
            }
        }
    }
}

impl std::error::Error for UxBlobError {}

impl From<UxBlobError> for diesel::result::Error {
    fn from(err: UxBlobError) -> Self {
        diesel::result::Error::DeserializationError(Box::new(err))
    }
}

macro_rules! define_uint_blob {
    ($name:ident, $type:ty) => {
        /// A wrapper that stores an unsigned integer as a fixed-size
        /// big-endian byte array.
        ///
        /// This encoding ensures that numeric ordering is preserved
        /// when values are stored as BLOBs and compared
        /// lexicographically, such as in SQLite.
        ///
        /// Internally, this wrapper has the same memory layout as the
        /// underlying integer due to `#[repr(transparent)]`.
        #[derive(
            Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, AsExpression, FromSqlRow,
        )]
        #[diesel(sql_type = Binary)]
        #[repr(transparent)]
        pub struct $name($type);

        impl $name {
            /// Returns a copy of the inner value.
            ///
            /// This method provides read access to the wrapped
            /// integer without consuming the blob.
            pub fn get(&self) -> $type {
                self.0
            }

            /// Consumes the blob, returning the inner value.
            ///
            /// Unlike [`Self::get()`], this method consumes the blob
            /// and returns ownership of the inner value.
            pub fn into_inner(self) -> $type {
                self.0
            }

            /// Converts the inner value to a byte vector in
            /// big-endian order.
            ///
            /// This method is used by [`Self::to_sql`] when
            /// serialising this type for Diesel's SQLite backend. It
            /// converts the inner value into a `Vec<u8>` formatted as
            /// a fixed-size, big-endian byte array suitable for use
            /// in SQLite BLOB columns.
            ///
            /// Note: This method allocates a new `Vec<u8>` each time.
            /// This allocation is required by Diesel's SQLite API,
            /// which expects owned data (`Vec<u8>`) in its
            /// [`Output::set_value`] method.
            fn to_bytes(self) -> Vec<u8> {
                self.0.to_be_bytes().to_vec()
            }

            /// Decodes a big-endian byte slice into the wrapped
            /// integer type.
            ///
            /// This method validates that the byte slice has exactly
            /// the expected length for the type and converts it to
            /// the target integer type in big-endian order. It uses
            /// [`TryInto`] to convert the slice to a
            /// fixed-size array, which automatically validates the
            /// length.
            fn from_bytes(bytes: &[u8]) -> Result<Self, UxBlobError> {
                const EXPECTED_SIZE: usize = std::mem::size_of::<$type>();

                let array: Result<[u8; EXPECTED_SIZE], _> = bytes.try_into();

                match array {
                    Ok(byte_array) => Ok($name(<$type>::from_be_bytes(byte_array))),
                    Err(_) => Err(UxBlobError::InvalidSize {
                        expected: EXPECTED_SIZE,
                        actual: bytes.len(),
                        type_name: std::any::type_name::<$type>().to_string(),
                    }),
                }
            }
        }

        /// Allows constructing this wrapper from the underlying type
        /// via [`From`].
        impl From<$type> for $name {
            fn from(value: $type) -> Self {
                $name(value)
            }
        }

        /// Implementation of [`diesel::serialize::ToSql<Binary,
        /// Sqlite>`] for Diesel integration.
        impl ToSql<Binary, Sqlite> for $name {
            fn to_sql<'b>(&'b self, out: &mut Output<'b, '_, Sqlite>) -> diesel::serialize::Result {
                out.set_value(self.to_bytes());
                Ok(IsNull::No)
            }
        }

        /// Implementation of [`diesel::deserialize::FromSql<Binary,
        /// Sqlite>`] for Diesel integration.
        impl FromSql<Binary, Sqlite> for $name {
            fn from_sql(
                bytes: <Sqlite as Backend>::RawValue<'_>,
            ) -> diesel::deserialize::Result<Self> {
                let blob = <Vec<u8> as FromSql<Binary, Sqlite>>::from_sql(bytes)?;
                Self::from_bytes(&blob).map_err(|e| e.into())
            }
        }

        // Display for consistent user-facing output.
        impl std::fmt::Display for $name {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                write!(
                    f,
                    "{:0width$x}",
                    self.0,
                    width = std::mem::size_of::<$type>() * 2
                )
            }
        }
    };
}

define_uint_blob!(U8Blob, u8);
define_uint_blob!(U16Blob, u16);
define_uint_blob!(U32Blob, u32);
define_uint_blob!(U64Blob, u64);
define_uint_blob!(U128Blob, u128);

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use diesel::{dsl::count_star, prelude::*, sqlite::SqliteConnection};

    use super::*;

    macro_rules! test_diesel_boundary_values {
        ($name:ident, $type:ty, $blob_type:ty) => {
            #[test]
            fn $name() {
                table! {
                    boundary_test (id) {
                        id -> Integer,
                        value -> Binary,
                    }
                }

                #[derive(Queryable)]
                #[allow(dead_code)]
                struct BoundaryTest {
                    id: i32,
                    value: $blob_type,
                }

                let mut conn = SqliteConnection::establish(":memory:").unwrap();
                diesel::sql_query(
                    "CREATE TABLE boundary_test (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
                )
                .execute(&mut conn)
                .unwrap();

                diesel::insert_into(boundary_test::table)
                    .values((
                        boundary_test::id.eq(1),
                        boundary_test::value.eq(<$blob_type>::from(0 as $type)),
                    ))
                    .execute(&mut conn)
                    .unwrap();

                diesel::insert_into(boundary_test::table)
                    .values((
                        boundary_test::id.eq(2),
                        boundary_test::value.eq(<$blob_type>::from(<$type>::MAX)),
                    ))
                    .execute(&mut conn)
                    .unwrap();

                let min_value = boundary_test::table
                    .find(1)
                    .first::<BoundaryTest>(&mut conn)
                    .unwrap();

                let max_value = boundary_test::table
                    .find(2)
                    .first::<BoundaryTest>(&mut conn)
                    .unwrap();

                assert_eq!(min_value.value.get(), 0 as $type);
                assert_eq!(max_value.value.get(), <$type>::MAX);
            }
        };
    }

    macro_rules! test_blob_generic {
        ($name:ident, $blob:ty, $val:expr) => {
            #[test]
            fn $name() {
                let blob: $blob = <$blob>::from($val);
                assert_eq!(<$blob>::from_bytes(&blob.to_bytes()).unwrap().get(), $val);
            }
        };
    }

    test_blob_generic!(roundtrip_u8, U8Blob, 42u8);
    test_blob_generic!(roundtrip_u128, U128Blob, u128::MAX);

    test_diesel_boundary_values!(test_u8_boundary_values, u8, U8Blob);
    test_diesel_boundary_values!(test_u16_boundary_values, u16, U16Blob);
    test_diesel_boundary_values!(test_u32_boundary_values, u32, U32Blob);
    test_diesel_boundary_values!(test_u64_boundary_values, u64, U64Blob);
    test_diesel_boundary_values!(test_u128_boundary_values, u128, U128Blob);

    #[cfg(test)]
    mod diesel_crud_operations {
        use super::*;

        table! {
            blob_crud (id) {
                id -> Integer,
                name -> Text,
                value_u8 -> Binary,
                value_u64 -> Binary,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = blob_crud)]
        struct CrudEntry {
            id: i32,
            name: String,
            value_u8: U8Blob,
            value_u64: U64Blob,
        }

        fn setup_crud() -> SqliteConnection {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE blob_crud (
                id INTEGER PRIMARY KEY,
                name TEXT NOT NULL,
                value_u8 BLOB NOT NULL,
                value_u64 BLOB NOT NULL
            )",
            )
            .execute(&mut conn)
            .unwrap();
            conn
        }

        #[test]
        fn test_crud_insertion() {
            let entry = CrudEntry {
                id: 1,
                name: "Test".into(),
                value_u8: U8Blob::from(42),
                value_u64: U64Blob::from(1_000),
            };

            let rows = diesel::insert_into(blob_crud::table)
                .values(&entry)
                .execute(&mut setup_crud())
                .unwrap();
            assert_eq!(rows, 1);
        }

        #[test]
        fn test_crud_read() {
            let mut conn = setup_crud();

            let entry = CrudEntry {
                id: 1,
                name: "ReadMe".into(),
                value_u8: U8Blob::from(7),
                value_u64: U64Blob::from(777),
            };

            diesel::insert_into(blob_crud::table)
                .values(&entry)
                .execute(&mut conn)
                .unwrap();

            let fetched: CrudEntry = blob_crud::table.find(1).first(&mut conn).unwrap();
            assert_eq!(fetched, entry);
        }

        #[test]
        fn test_crud_filter() {
            let mut conn = setup_crud();

            let entries = vec![
                CrudEntry {
                    id: 1,
                    name: "A".into(),
                    value_u8: U8Blob::from(10),
                    value_u64: U64Blob::from(100),
                },
                CrudEntry {
                    id: 2,
                    name: "B".into(),
                    value_u8: U8Blob::from(20),
                    value_u64: U64Blob::from(200),
                },
            ];

            diesel::insert_into(blob_crud::table)
                .values(&entries)
                .execute(&mut conn)
                .unwrap();

            let filtered: Vec<CrudEntry> = blob_crud::table
                .filter(blob_crud::value_u8.eq(U8Blob::from(20)))
                .load(&mut conn)
                .unwrap();

            assert_eq!(filtered.len(), 1);
            assert_eq!(filtered[0].id, 2);
        }

        #[test]
        fn test_crud_update() {
            let mut conn = setup_crud();

            let entry = CrudEntry {
                id: 1,
                name: "Updatable".into(),
                value_u8: U8Blob::from(5),
                value_u64: U64Blob::from(500),
            };

            diesel::insert_into(blob_crud::table)
                .values(&entry)
                .execute(&mut conn)
                .unwrap();

            let updated = diesel::update(blob_crud::table.find(1))
                .set(blob_crud::value_u8.eq(U8Blob::from(55)))
                .execute(&mut conn)
                .unwrap();

            assert_eq!(updated, 1);
            let fetched: CrudEntry = blob_crud::table.find(1).first(&mut conn).unwrap();
            assert_eq!(fetched.value_u8.get(), 55);
        }

        #[test]
        fn test_crud_delete() {
            let mut conn = setup_crud();

            let entry = CrudEntry {
                id: 1,
                name: "Deletable".into(),
                value_u8: U8Blob::from(9),
                value_u64: U64Blob::from(900),
            };

            diesel::insert_into(blob_crud::table)
                .values(&entry)
                .execute(&mut conn)
                .unwrap();

            let deleted =
                diesel::delete(blob_crud::table.filter(blob_crud::value_u8.eq(U8Blob::from(9))))
                    .execute(&mut conn)
                    .unwrap();

            assert_eq!(deleted, 1);
            let remaining: Vec<CrudEntry> = blob_crud::table.load(&mut conn).unwrap();
            assert!(remaining.is_empty());
        }
    }

    #[cfg(test)]
    mod diesel_edge_cases {
        use super::*;

        // Type mismatch when reading wrong-sized BLOB.
        table! {
            type_test (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Debug, Insertable, Queryable)]
        #[diesel(table_name = type_test)]
        struct TypeTestU16 {
            id: i32,
            value: U16Blob,
        }

        #[derive(Debug, Insertable, Queryable)]
        #[diesel(table_name = type_test)]
        struct TypeTestU32 {
            id: i32,
            value: U32Blob,
        }

        #[test]
        fn diesel_type_mismatch_read() {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE type_test (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
            )
            .execute(&mut conn)
            .unwrap();

            diesel::insert_into(type_test::table)
                .values(&TypeTestU16 {
                    id: 1,
                    value: U16Blob::from(12345),
                })
                .execute(&mut conn)
                .unwrap();

            let result = type_test::table.find(1).first::<TypeTestU32>(&mut conn);

            assert!(result.is_err());
            assert!(
                result
                    .unwrap_err()
                    .to_string()
                    .contains("Invalid input size")
            );
        }

        // Empty and undersized BLOBs.
        table! {
            blob_test (id) {
                id -> Integer,
                size_type -> Text,
                value -> Binary,
            }
        }

        #[derive(Debug, Queryable)]
        #[allow(dead_code)]
        struct BlobTestU8 {
            id: i32,
            size_type: String,
            value: U8Blob,
        }
        #[derive(Debug, Queryable)]
        #[allow(dead_code)]
        struct BlobTestU16 {
            id: i32,
            size_type: String,
            value: U16Blob,
        }

        #[test]
        fn diesel_empty_and_undersized_blob() {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE blob_test (id INTEGER PRIMARY KEY, size_type TEXT, value BLOB)",
            )
            .execute(&mut conn)
            .unwrap();

            diesel::sql_query(
                "INSERT INTO blob_test (id, size_type, value) VALUES (1, 'empty', X'')",
            )
            .execute(&mut conn)
            .unwrap();

            diesel::sql_query(
                "INSERT INTO blob_test (id, size_type, value) VALUES (2, 'undersized', X'2A')",
            )
            .execute(&mut conn)
            .unwrap();

            let err_empty = blob_test::table
                .filter(blob_test::size_type.eq("empty"))
                .first::<BlobTestU8>(&mut conn);

            assert!(err_empty.is_err());
            assert!(
                err_empty
                    .unwrap_err()
                    .to_string()
                    .contains("Invalid input size")
            );

            let err_undersized = blob_test::table
                .filter(blob_test::size_type.eq("undersized"))
                .first::<BlobTestU16>(&mut conn);

            assert!(err_undersized.is_err());
            assert!(
                err_undersized
                    .unwrap_err()
                    .to_string()
                    .contains("Invalid input size")
            );
        }

        // Update with wrong size.
        table! {
            update_test (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Debug, Insertable, Queryable, Identifiable, AsChangeset)]
        #[diesel(table_name = update_test)]
        struct UpdateTestU16 {
            id: i32,
            value: U16Blob,
        }

        #[test]
        fn diesel_update_wrong_size() {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE update_test (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
            )
            .execute(&mut conn)
            .unwrap();

            diesel::insert_into(update_test::table)
                .values(&UpdateTestU16 {
                    id: 1,
                    value: U16Blob::from(42),
                })
                .execute(&mut conn)
                .unwrap();

            // DB-level update succeeds.
            let update_ok = diesel::update(update_test::table.find(1))
                .set(update_test::value.eq(U32Blob::from(43)))
                .execute(&mut conn);

            assert!(update_ok.is_ok());

            // Retrieval fails due to wrong blob size.
            let err = update_test::table.find(1).first::<UpdateTestU16>(&mut conn);
            assert!(err.is_err());
            assert!(err.unwrap_err().to_string().contains("Invalid input size"));
        }
    }

    #[cfg(test)]
    mod diesel_query_operations {
        use super::*;

        table! {
            blob_query (id) {
                id -> Integer,
                category -> Text,
                value_u8 -> Binary,
                value_u16 -> Binary,
                value_u32 -> Binary,
                value_u64 -> Binary,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = blob_query)]
        struct BlobQueryEntry {
            id: i32,
            category: String,
            value_u8: U8Blob,
            value_u16: U16Blob,
            value_u32: U32Blob,
            value_u64: U64Blob,
        }

        fn setup_test_data() -> SqliteConnection {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE blob_query (
                id INTEGER PRIMARY KEY,
                category TEXT NOT NULL,
                value_u8 BLOB NOT NULL,
                value_u16 BLOB NOT NULL,
                value_u32 BLOB NOT NULL,
                value_u64 BLOB NOT NULL
            )",
            )
            .execute(&mut conn)
            .unwrap();

            let entries = vec![
                BlobQueryEntry {
                    id: 1,
                    category: "A".into(),
                    value_u8: 10.into(),
                    value_u16: 1000.into(),
                    value_u32: 100_000.into(),
                    value_u64: 10_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 2,
                    category: "A".into(),
                    value_u8: 20.into(),
                    value_u16: 2000.into(),
                    value_u32: 200_000.into(),
                    value_u64: 20_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 3,
                    category: "A".into(),
                    value_u8: 30.into(),
                    value_u16: 3000.into(),
                    value_u32: 300_000.into(),
                    value_u64: 30_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 4,
                    category: "B".into(),
                    value_u8: 100.into(),
                    value_u16: 10_000.into(),
                    value_u32: 1_000_000.into(),
                    value_u64: 40_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 5,
                    category: "B".into(),
                    value_u8: 150.into(),
                    value_u16: 15_000.into(),
                    value_u32: 1_500_000.into(),
                    value_u64: 50_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 6,
                    category: "C".into(),
                    value_u8: 0.into(),
                    value_u16: 0.into(),
                    value_u32: 0.into(),
                    value_u64: 0.into(),
                },
                BlobQueryEntry {
                    id: 7,
                    category: "C".into(),
                    value_u8: 255.into(),
                    value_u16: u16::MAX.into(),
                    value_u32: u32::MAX.into(),
                    value_u64: (i64::MAX as u64).into(),
                },
                BlobQueryEntry {
                    id: 8,
                    category: "D".into(),
                    value_u8: 50.into(),
                    value_u16: 5000.into(),
                    value_u32: 500_000.into(),
                    value_u64: 5_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 9,
                    category: "D".into(),
                    value_u8: 50.into(),
                    value_u16: 5000.into(),
                    value_u32: 600_000.into(),
                    value_u64: 6_000_000_000.into(),
                },
                BlobQueryEntry {
                    id: 10,
                    category: "D".into(),
                    value_u8: 50.into(),
                    value_u16: 6000.into(),
                    value_u32: 700_000.into(),
                    value_u64: 7_000_000_000.into(),
                },
            ];

            diesel::insert_into(blob_query::table)
                .values(&entries)
                .execute(&mut conn)
                .unwrap();

            conn
        }

        #[test]
        fn test_insertion_and_counting() {
            let counts: HashMap<String, usize> = blob_query::table
                .load(&mut setup_test_data())
                .unwrap()
                .into_iter()
                .fold(HashMap::new(), |mut acc, row: BlobQueryEntry| {
                    *acc.entry(row.category).or_insert(0) += 1;
                    acc
                });

            let expected: HashMap<String, usize> = HashMap::from([
                ("A".to_string(), 3),
                ("B".to_string(), 2),
                ("C".to_string(), 2),
                ("D".to_string(), 3),
            ]);

            assert_eq!(counts, expected);
        }

        #[test]
        fn test_filter_eq() {
            assert_eq!(
                blob_query::table
                    .filter(blob_query::value_u8.eq(U8Blob::from(50)))
                    .load::<BlobQueryEntry>(&mut setup_test_data())
                    .unwrap()
                    .len(),
                3
            );
        }

        #[test]
        fn test_filter_ne() {
            assert_eq!(
                blob_query::table
                    .filter(blob_query::value_u8.ne(U8Blob::from(50)))
                    .load::<BlobQueryEntry>(&mut setup_test_data())
                    .unwrap()
                    .len(),
                7
            );
        }

        #[test]
        fn test_filter_range() {
            assert_eq!(
                blob_query::table
                    .filter(blob_query::value_u16.ge(U16Blob::from(2000)))
                    .filter(blob_query::value_u16.lt(U16Blob::from(10000)))
                    .load::<BlobQueryEntry>(&mut setup_test_data())
                    .unwrap()
                    .len(),
                5
            );
        }

        #[test]
        fn test_filter_or() {
            assert_eq!(
                blob_query::table
                    .filter(
                        blob_query::value_u8
                            .eq(U8Blob::from(0))
                            .or(blob_query::value_u8.eq(U8Blob::from(255)))
                    )
                    .load::<BlobQueryEntry>(&mut setup_test_data())
                    .unwrap()
                    .len(),
                2
            );
        }

        #[test]
        fn test_count_filter() {
            let count: i64 = blob_query::table
                .filter(blob_query::value_u8.gt(U8Blob::from(50)))
                .count()
                .get_result(&mut setup_test_data())
                .unwrap();
            assert_eq!(count, 3);
        }

        #[test]
        fn test_group_by() {
            let results: Vec<(String, i64)> = blob_query::table
                .group_by(blob_query::category)
                .select((blob_query::category, count_star()))
                .order(blob_query::category.asc())
                .load(&mut setup_test_data())
                .unwrap();
            assert_eq!(
                results,
                vec![
                    ("A".into(), 3),
                    ("B".into(), 2),
                    ("C".into(), 2),
                    ("D".into(), 3)
                ]
            );
        }

        #[test]
        fn test_distinct_values() {
            let distinct: Vec<U8Blob> = blob_query::table
                .select(blob_query::value_u8)
                .filter(blob_query::category.eq("D"))
                .distinct()
                .load(&mut setup_test_data())
                .unwrap();
            assert_eq!(distinct, vec![U8Blob::from(50)]);
        }

        #[test]
        fn test_order_ascending_u16() {
            let vals: Vec<_> = blob_query::table
                .filter(blob_query::category.eq("A"))
                .order(blob_query::value_u16.asc())
                .load(&mut setup_test_data())
                .unwrap()
                .iter()
                .map(|e: &BlobQueryEntry| e.value_u16.get())
                .collect();
            assert_eq!(vals, vec![1000, 2000, 3000]);
        }

        #[test]
        fn test_order_descending_u64() {
            let first = blob_query::table
                .order(blob_query::value_u64.desc())
                .limit(1)
                .first::<BlobQueryEntry>(&mut setup_test_data())
                .unwrap();
            assert_eq!(first.value_u64.get(), 9_223_372_036_854_775_807u64);
        }

        #[test]
        fn test_complex_filtering() {
            let mut conn = setup_test_data();
            let entries = blob_query::table
                .filter(
                    blob_query::category
                        .eq("A")
                        .and(blob_query::value_u16.lt(U16Blob::from(3000)))
                        .or(blob_query::category
                            .eq("B")
                            .and(blob_query::value_u8.gt(U8Blob::from(100)))),
                )
                .order(blob_query::id.asc())
                .load::<BlobQueryEntry>(&mut conn)
                .unwrap();
            assert_eq!(
                entries.iter().map(|e| e.id).collect::<Vec<_>>(),
                vec![1, 2, 5]
            );
        }
    }

    #[cfg(test)]
    mod diesel_null_handling {
        use super::*;

        table! {
            nullable_blobs (id) {
                id -> Integer,
                name -> Text,
                optional_value -> Nullable<Binary>,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = nullable_blobs)]
        struct NullableEntry {
            id: i32,
            name: String,
            optional_value: Option<U32Blob>,
        }

        fn setup_nullable_table() -> SqliteConnection {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();

            diesel::sql_query(
                "CREATE TABLE nullable_blobs (
                id INTEGER PRIMARY KEY,
                name TEXT NOT NULL,
                optional_value BLOB
            )",
            )
            .execute(&mut conn)
            .unwrap();

            conn
        }

        #[test]
        fn test_insert_and_read_null_values() {
            let mut conn = setup_nullable_table();

            // Insert entries with and without values.
            let entries = vec![
                NullableEntry {
                    id: 1,
                    name: "Has Value".into(),
                    optional_value: Some(U32Blob::from(42u32)),
                },
                NullableEntry {
                    id: 2,
                    name: "No Value".into(),
                    optional_value: None,
                },
            ];

            diesel::insert_into(nullable_blobs::table)
                .values(&entries)
                .execute(&mut conn)
                .unwrap();

            // Test reading entries.
            let results: Vec<NullableEntry> = nullable_blobs::table
                .order_by(nullable_blobs::id)
                .load(&mut conn)
                .unwrap();

            assert_eq!(results.len(), 2);
            assert_eq!(results[0].optional_value, Some(U32Blob::from(42u32)));
            assert_eq!(results[1].optional_value, None);
        }

        #[test]
        fn test_filter_null_values() {
            let mut conn = setup_nullable_table();

            diesel::insert_into(nullable_blobs::table)
                .values(&vec![
                    NullableEntry {
                        id: 1,
                        name: "One".into(),
                        optional_value: Some(U32Blob::from(1u32)),
                    },
                    NullableEntry {
                        id: 2,
                        name: "Two".into(),
                        optional_value: None,
                    },
                    NullableEntry {
                        id: 3,
                        name: "Three".into(),
                        optional_value: Some(U32Blob::from(3u32)),
                    },
                ])
                .execute(&mut conn)
                .unwrap();

            // Test filtering for NULL values.
            let null_entries: Vec<NullableEntry> = nullable_blobs::table
                .filter(nullable_blobs::optional_value.is_null())
                .load(&mut conn)
                .unwrap();

            assert_eq!(null_entries.len(), 1);
            assert_eq!(null_entries[0].id, 2);

            // Test filtering for non-NULL values.
            let non_null_entries: Vec<NullableEntry> = nullable_blobs::table
                .filter(nullable_blobs::optional_value.is_not_null())
                .load(&mut conn)
                .unwrap();

            assert_eq!(non_null_entries.len(), 2);
            assert!(non_null_entries.iter().all(|e| e.optional_value.is_some()));
        }

        #[test]
        fn test_update_null_values() {
            let mut conn = setup_nullable_table();

            diesel::insert_into(nullable_blobs::table)
                .values(&vec![
                    NullableEntry {
                        id: 1,
                        name: "Initially has value".into(),
                        optional_value: Some(U32Blob::from(42u32)),
                    },
                    NullableEntry {
                        id: 2,
                        name: "Initially null".into(),
                        optional_value: None,
                    },
                ])
                .execute(&mut conn)
                .unwrap();

            // Update: value -> NULL.
            diesel::update(nullable_blobs::table.find(1))
                .set(nullable_blobs::optional_value.eq::<Option<U32Blob>>(None))
                .execute(&mut conn)
                .unwrap();

            // Update: NULL -> value
            diesel::update(nullable_blobs::table.find(2))
                .set(nullable_blobs::optional_value.eq(Some(U32Blob::from(100u32))))
                .execute(&mut conn)
                .unwrap();

            let results: Vec<NullableEntry> = nullable_blobs::table
                .order_by(nullable_blobs::id)
                .load(&mut conn)
                .unwrap();

            assert_eq!(results[0].optional_value, None);
            assert_eq!(results[1].optional_value, Some(U32Blob::from(100u32)));
        }
    }

    #[test]
    fn test_malicious_looking_blob() {
        table! {
            x (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = x)]
        struct Row {
            id: i32,
            value: U128Blob,
        }

        const MALICIOUS_LOOKING_BLOB: [u8; 16] = *b"DROP TABLE x;\0\0\0";

        let blob = U128Blob::from_bytes(&MALICIOUS_LOOKING_BLOB).unwrap();

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE x (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        let inserted = Row { id: 1, value: blob };
        diesel::insert_into(x::table)
            .values(&inserted)
            .execute(&mut conn)
            .unwrap();

        let retrieved: Row = x::table.filter(x::id.eq(1)).first(&mut conn).unwrap();
        assert_eq!(inserted, retrieved);
    }
}
