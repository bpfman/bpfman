// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! A wrapper type for handling kernel-provided `u32` values in SQLite
//! databases.
//!
//! # Overview
//!
//! `KernelU32` addresses a specific use case: storing kernel-provided
//! `u32` values in SQLite using Diesel, where:
//!
//! 1. SQLite has no native unsigned integer types
//! 2. Kernel interfaces commonly return `u32` values
//! 3. Sometime those values must be round-tripped back to `u32` for
//!    kernel calls
//!
//! # Purpose
//!
//! While all `u32` values can be safely stored as `i64` in SQLite
//! (using BIGINT columns), downcasting from `i64` to `u32` is
//! potentially unsafe if the value exceeds the `u32` range. This
//! wrapper provides the necessary runtime type safety checks when
//! these conversions happen.
//!
//! For fields that need to be round-tripped from Rust `u32` -> SQLite
//! `BIGINT` -> Rust `u32` (typically for passing to kernel APIs), you
//! should:
//!
//! 1. Declare the database column as `BIGINT` in your schema
//! 2. Use `KernelU32` as the field type in your Rust struct
//! 3. Use `try_u32()` when retrieving values for kernel calls
//!
//! # Value Handling
//!
//! * A `u32` ranges from 0 to 4,294,967,295
//! * An `i64` can represent this entire range without loss of precision
//!
//! When retrieving a value:
//! * `get()` returns the value as `u32`, panicking if out of range
//! * `try_u32()` returns a `Result<u32, KernelU32Error>`, allowing error handling
//!
//! # Diesel Integration
//!
//! `KernelU32` implements:
//! * `ToSql<BigInt, Sqlite>`
//! * `FromSql<BigInt, Sqlite>`
//! * `AsExpression<BigInt>`
//! * `FromSqlRow<BigInt>`

/// Errors that can occur when working with KernelU32 values.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum KernelU32Error {
    /// The i64 value is outside the valid range for u32 (0 to
    /// [`u32::MAX`]).
    OutOfRange {
        /// The invalid value that was encountered.
        value: i64,
    },
}

impl std::fmt::Display for KernelU32Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            KernelU32Error::OutOfRange { value } => {
                write!(
                    f,
                    "Value {} is outside the valid range for u32 (0 to {})",
                    value,
                    u32::MAX,
                )
            }
        }
    }
}

impl std::error::Error for KernelU32Error {}

#[derive(
    Default,
    Debug,
    Clone,
    Copy,
    PartialEq,
    Eq,
    Hash,
    serde::Serialize,
    serde::Deserialize,
    diesel::expression::AsExpression,
    diesel::deserialize::FromSqlRow,
)]
#[diesel(sql_type = diesel::sql_types::BigInt)]
#[repr(transparent)]
pub struct KernelU32(i64);

impl KernelU32 {
    /// Returns the value as `u32`, panicking if out of range.
    ///
    /// For fallible conversion, use [`Self::try_u32()`] instead.
    pub fn get(self) -> u32 {
        self.try_u32().expect("KernelU32: value out of u32 range")
    }

    /// Try to extract `u32`, returning an error if the stored `i64`
    /// is outside the valid range for `u32`.
    pub fn try_u32(self) -> Result<u32, KernelU32Error> {
        u32::try_from(self.0).map_err(|_| KernelU32Error::OutOfRange { value: self.0 })
    }

    /// Returns the inner i64 value.
    pub fn as_i64(self) -> i64 {
        self.0
    }
}

impl std::fmt::Display for KernelU32 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        // Using `get()` is acceptable because KernelU32 should always
        // hold a valid u32.
        write!(f, "{}", self.get())
    }
}

impl From<u32> for KernelU32 {
    fn from(val: u32) -> Self {
        Self(val as i64)
    }
}

impl From<KernelU32> for i64 {
    fn from(val: KernelU32) -> Self {
        val.0
    }
}

impl TryFrom<i64> for KernelU32 {
    type Error = KernelU32Error;
    fn try_from(val: i64) -> Result<Self, Self::Error> {
        u32::try_from(val)
            .map(|_| Self(val))
            .map_err(|_| KernelU32Error::OutOfRange { value: val })
    }
}

impl diesel::serialize::ToSql<diesel::sql_types::BigInt, diesel::sqlite::Sqlite> for KernelU32 {
    fn to_sql<'b>(
        &'b self,
        out: &mut diesel::serialize::Output<'b, '_, diesel::sqlite::Sqlite>,
    ) -> diesel::serialize::Result {
        diesel::serialize::ToSql::<diesel::sql_types::BigInt, diesel::sqlite::Sqlite>::to_sql(
            &self.0, out,
        )
    }
}

impl diesel::deserialize::FromSql<diesel::sql_types::BigInt, diesel::sqlite::Sqlite> for KernelU32 {
    fn from_sql(
        bytes: <diesel::sqlite::Sqlite as diesel::backend::Backend>::RawValue<'_>,
    ) -> diesel::deserialize::Result<Self> {
        let value = i64::from_sql(bytes)?;
        Self::try_from(value).map_err(|e| {
            Box::new(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("Invalid KernelU32 value: {}", e),
            )) as Box<dyn std::error::Error + Send + Sync>
        })
    }
}

#[cfg(test)]
mod tests {
    use std::convert::TryFrom;

    use diesel::{prelude::*, sqlite::SqliteConnection};

    use super::*;

    // Basic Conversion Tests.

    #[test]
    fn test_k32_from_u32() {
        let val = 123u32;
        let wrapped = KernelU32::from(val);
        assert_eq!(wrapped.get(), val);
        assert_eq!(wrapped.as_i64(), val as i64);
    }

    #[test]
    fn test_k32_into_i64() {
        let val = 987u32;
        let wrapped = KernelU32::from(val);
        let raw: i64 = wrapped.into();
        assert_eq!(raw, val as i64);
    }

    // Boundary Tests.

    #[test]
    fn test_k32_boundary_values() {
        // Test minimum value (0).
        let min_val = 0u32;
        let wrapped_min = KernelU32::from(min_val);
        assert_eq!(wrapped_min.get(), min_val);

        // Test maximum value (u32::MAX).
        let max_val = u32::MAX;
        let wrapped_max = KernelU32::from(max_val);
        assert_eq!(wrapped_max.get(), max_val);
    }

    // TryFrom Tests.

    #[test]
    fn test_k32_try_from_i64_valid() {
        let val: i64 = u32::MAX as i64;
        let wrapped = KernelU32::try_from(val).unwrap();
        assert_eq!(wrapped.get(), u32::MAX);
    }

    #[test]
    fn test_k32_try_from_i64_invalid_negative() {
        let val: i64 = -1;
        let result = KernelU32::try_from(val);
        assert!(result.is_err());

        match result {
            Err(KernelU32Error::OutOfRange { .. }) => {}
            _ => panic!("Expected OutOfRange error"),
        }
    }

    #[test]
    fn test_k32_try_from_i64_invalid_too_large() {
        let val: i64 = u32::MAX as i64 + 1;
        let result = KernelU32::try_from(val);
        assert!(result.is_err());

        match result {
            Err(KernelU32Error::OutOfRange { .. }) => {}
            _ => panic!("Expected OutOfRange error"),
        }
    }

    // try_u32 Method Tests.

    #[test]
    fn test_k32_try_u32() {
        // Valid case.
        let valid = KernelU32::from(42u32);
        assert_eq!(valid.try_u32().unwrap(), 42u32);

        // Invalid case (negative).
        let negative = KernelU32(-1);
        assert!(negative.try_u32().is_err());

        // Invalid case (too large).
        let too_large = KernelU32(i64::MAX);
        assert!(too_large.try_u32().is_err());

        // Check the error type.
        match too_large.try_u32() {
            Err(KernelU32Error::OutOfRange { .. }) => {}
            _ => panic!("Expected OutOfRange error"),
        }
    }

    // Panic Tests.

    #[test]
    #[should_panic(expected = "KernelU32: value out of u32 range")]
    fn test_k32_get_panics_if_out_of_range() {
        let bad = KernelU32(i64::MAX);
        let _ = bad.get(); // should panic
    }

    // Serde Tests.

    #[test]
    fn test_k32_serde() {
        let original = KernelU32::from(123u32);

        // Serialize to JSON.
        let serialized = serde_json::to_string(&original).unwrap();
        assert_eq!(serialized, "123"); // Value should be serialized as its inner i64

        // Deserialize from JSON.
        let deserialized: KernelU32 = serde_json::from_str(&serialized).unwrap();
        assert_eq!(deserialized.get(), 123u32);

        // Verify round-trip.
        assert_eq!(original, deserialized);
    }

    // Diesel Integration Tests.

    mod diesel_integration {
        use super::*;

        table! {
            k32_test (id) {
                id -> Integer,
                value -> BigInt,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = k32_test)]
        struct KernelU32Test {
            id: i32,
            value: KernelU32,
        }

        fn setup_db() -> SqliteConnection {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();
            diesel::sql_query(
                "CREATE TABLE k32_test (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)",
            )
            .execute(&mut conn)
            .unwrap();
            conn
        }

        #[test]
        fn test_diesel_k32_crud_operations() {
            let mut conn = setup_db();

            // Insert test.
            let entry = KernelU32Test {
                id: 1,
                value: KernelU32::from(42u32),
            };
            diesel::insert_into(k32_test::table)
                .values(&entry)
                .execute(&mut conn)
                .unwrap();

            // Query test.
            let result: KernelU32Test = k32_test::table.find(1).first(&mut conn).unwrap();
            assert_eq!(result.value.get(), 42u32);

            // Filter test.
            let filtered: Vec<KernelU32Test> = k32_test::table
                .filter(k32_test::value.eq(KernelU32::from(42u32)))
                .load(&mut conn)
                .unwrap();
            assert_eq!(filtered.len(), 1);

            // Update test.
            diesel::update(k32_test::table.find(1))
                .set(k32_test::value.eq(KernelU32::from(100u32)))
                .execute(&mut conn)
                .unwrap();

            let updated: KernelU32Test = k32_test::table.find(1).first(&mut conn).unwrap();
            assert_eq!(updated.value.get(), 100u32);

            // Delete test.
            diesel::delete(k32_test::table.find(1))
                .execute(&mut conn)
                .unwrap();

            let results: Vec<KernelU32Test> = k32_test::table.load(&mut conn).unwrap();
            assert!(results.is_empty());
        }

        #[test]
        fn test_diesel_k32_boundary_values() {
            let mut conn = setup_db();

            // Insert minimum and maximum values.
            diesel::insert_into(k32_test::table)
                .values(&[
                    KernelU32Test {
                        id: 1,
                        value: KernelU32::from(0u32),
                    },
                    KernelU32Test {
                        id: 2,
                        value: KernelU32::from(u32::MAX),
                    },
                ])
                .execute(&mut conn)
                .unwrap();

            // Verify values were stored and retrieved correctly.
            let min_val: KernelU32Test = k32_test::table.find(1).first(&mut conn).unwrap();
            let max_val: KernelU32Test = k32_test::table.find(2).first(&mut conn).unwrap();

            assert_eq!(min_val.value.get(), 0u32);
            assert_eq!(max_val.value.get(), u32::MAX);
        }

        #[test]
        fn test_diesel_k32_ordering() {
            let mut conn = setup_db();

            // Insert values in non-sequential order.
            diesel::insert_into(k32_test::table)
                .values(&[
                    KernelU32Test {
                        id: 1,
                        value: KernelU32::from(300u32),
                    },
                    KernelU32Test {
                        id: 2,
                        value: KernelU32::from(100u32),
                    },
                    KernelU32Test {
                        id: 3,
                        value: KernelU32::from(200u32),
                    },
                ])
                .execute(&mut conn)
                .unwrap();

            // Test ordering (ascending).
            let asc_results: Vec<KernelU32Test> = k32_test::table
                .order(k32_test::value.asc())
                .load(&mut conn)
                .unwrap();

            assert_eq!(asc_results[0].value.get(), 100u32);
            assert_eq!(asc_results[1].value.get(), 200u32);
            assert_eq!(asc_results[2].value.get(), 300u32);

            // Test ordering (descending).
            let desc_results: Vec<KernelU32Test> = k32_test::table
                .order(k32_test::value.desc())
                .load(&mut conn)
                .unwrap();

            assert_eq!(desc_results[0].value.get(), 300u32);
            assert_eq!(desc_results[1].value.get(), 200u32);
            assert_eq!(desc_results[2].value.get(), 100u32);
        }

        #[test]
        fn test_diesel_k32_comparisons() {
            let mut conn = setup_db();

            // Insert test values.
            diesel::insert_into(k32_test::table)
                .values(&[
                    KernelU32Test {
                        id: 1,
                        value: KernelU32::from(100u32),
                    },
                    KernelU32Test {
                        id: 2,
                        value: KernelU32::from(200u32),
                    },
                    KernelU32Test {
                        id: 3,
                        value: KernelU32::from(300u32),
                    },
                    KernelU32Test {
                        id: 4,
                        value: KernelU32::from(400u32),
                    },
                    KernelU32Test {
                        id: 5,
                        value: KernelU32::from(500u32),
                    },
                ])
                .execute(&mut conn)
                .unwrap();

            // Test greater than.
            let gt_results: Vec<KernelU32Test> = k32_test::table
                .filter(k32_test::value.gt(KernelU32::from(300u32)))
                .load(&mut conn)
                .unwrap();
            assert_eq!(gt_results.len(), 2);

            // Test less than or equal to.
            let lte_results: Vec<KernelU32Test> = k32_test::table
                .filter(k32_test::value.le(KernelU32::from(300u32)))
                .load(&mut conn)
                .unwrap();
            assert_eq!(lte_results.len(), 3);

            // Test range (between).
            let between_results: Vec<KernelU32Test> = k32_test::table
                .filter(k32_test::value.gt(KernelU32::from(100u32)))
                .filter(k32_test::value.lt(KernelU32::from(400u32)))
                .load(&mut conn)
                .unwrap();
            assert_eq!(between_results.len(), 2);
        }
    }

    // Optional Null Handling Tests.

    mod diesel_null_handling {
        use super::*;

        table! {
            nullable_k32 (id) {
                id -> Integer,
                value -> Nullable<BigInt>,
            }
        }

        #[derive(Debug, PartialEq, Queryable, Insertable)]
        #[diesel(table_name = nullable_k32)]
        struct NullableKernelU32Test {
            id: i32,
            value: Option<KernelU32>,
        }

        fn setup_nullable_db() -> SqliteConnection {
            let mut conn = SqliteConnection::establish(":memory:").unwrap();
            diesel::sql_query("CREATE TABLE nullable_k32 (id INTEGER PRIMARY KEY, value INTEGER)")
                .execute(&mut conn)
                .unwrap();
            conn
        }

        #[test]
        fn test_diesel_k32_null_handling() {
            let mut conn = setup_nullable_db();

            // Insert with and without values.
            diesel::insert_into(nullable_k32::table)
                .values(&[
                    NullableKernelU32Test {
                        id: 1,
                        value: Some(KernelU32::from(42u32)),
                    },
                    NullableKernelU32Test { id: 2, value: None },
                ])
                .execute(&mut conn)
                .unwrap();

            // Retrieve entries.
            let with_value: NullableKernelU32Test =
                nullable_k32::table.find(1).first(&mut conn).unwrap();
            let without_value: NullableKernelU32Test =
                nullable_k32::table.find(2).first(&mut conn).unwrap();

            assert_eq!(with_value.value.unwrap().get(), 42u32);
            assert_eq!(without_value.value, None);

            // Filter for NULL and NOT NULL.
            let null_results: Vec<NullableKernelU32Test> = nullable_k32::table
                .filter(nullable_k32::value.is_null())
                .load(&mut conn)
                .unwrap();
            assert_eq!(null_results.len(), 1);
            assert_eq!(null_results[0].id, 2);

            let non_null_results: Vec<NullableKernelU32Test> = nullable_k32::table
                .filter(nullable_k32::value.is_not_null())
                .load(&mut conn)
                .unwrap();
            assert_eq!(non_null_results.len(), 1);
            assert_eq!(non_null_results[0].id, 1);

            // Update NULL to value.
            diesel::update(nullable_k32::table.find(2))
                .set(nullable_k32::value.eq(Some(KernelU32::from(100u32))))
                .execute(&mut conn)
                .unwrap();

            let updated: NullableKernelU32Test =
                nullable_k32::table.find(2).first(&mut conn).unwrap();
            assert_eq!(updated.value.unwrap().get(), 100u32);

            // Update value to NULL.
            diesel::update(nullable_k32::table.find(1))
                .set(nullable_k32::value.eq::<Option<KernelU32>>(None))
                .execute(&mut conn)
                .unwrap();

            let nullified: NullableKernelU32Test =
                nullable_k32::table.find(1).first(&mut conn).unwrap();
            assert_eq!(nullified.value, None);
        }
    }

    // Edge Case Handling.

    #[test]
    fn test_k32_exact_boundary_cases() {
        // Test value exactly at u32::MAX.
        let max_val = KernelU32::try_from(u32::MAX as i64).unwrap();
        assert_eq!(max_val.get(), u32::MAX);

        // Test value exactly one more than u32::MAX.
        let overflow_val = KernelU32::try_from((u32::MAX as i64) + 1);
        assert!(overflow_val.is_err());

        // Test value exactly at 0.
        let min_val = KernelU32::try_from(0i64).unwrap();
        assert_eq!(min_val.get(), 0);

        // Test value exactly one less than 0.
        let underflow_val = KernelU32::try_from(-1i64);
        assert!(underflow_val.is_err());
    }
}
