//! This module provides `UIntBlob<T>`, a type for storing an unsigned
//! integer as a SQLite BLOB. We always encode the integer in its
//! big-endian form, using exactly the number of bytes dictated by
//! `T`. This ensures two things:
//!
//! 1. Lexicographical comparisons in SQLite correctly reflect numeric
//!    ordering (thanks to big-endian storage).
//! 2. Each integer type is stored with the minimal number of bytes
//!    needed so no accidental padding or truncation.
//!
//! Because `UIntBlob<T>` restricts `T` to the built-in unsigned
//! types (`u8`, `u16`, `u32`, `u64`, and `u128`), each BLOB
//! precisely matches the size of that type in bytes. If the stored
//! data has a mismatched length at query time, attempts to
//! deserialise it will fail immediately. This prevents silent data
//! corruption and type confusion.
//!
//! # Usage
//!
//! You can freely derive Diesel traits (like `Insertable`,
//! `Queryable`) for any struct containing `UIntBlob<T>`. When
//! inserting, updating, or retrieving from a `BLOB` column, your
//! application code stays strongly typed. For example, a
//! `UIntBlob<u16>` column always occupies exactly two bytes of
//! storage. Retrieving it as `UIntBlob<u64>` will fail if the
//! database actually stored eight bytes, preventing subtle
//! mis-match errors in your application logic.
//!
//! This design allows you to compare and sort values in SQLite using
//! the native BLOB comparison, yet obtain correct numerical ordering
//! because big-endian format preserves numerical magnitude in a
//! lexicographical sort.
//!
//! # Examples
//!
//! ```rust
//! use diesel::prelude::*;
//! use bpfman::uintblob::{UIntBlob, U64Blob};
//! # use diesel::sql_types::Binary;
//! # table! {
//! #     demo_table (id) {
//! #         id -> Integer,
//! #         counter -> Binary,
//! #     }
//! # }
//!
//! #[derive(Insertable, Queryable, Debug)]
//! #[diesel(table_name = demo_table)]
//! struct DemoRow {
//!     pub id: i32,
//!     pub counter: U64Blob,
//! }
//!
//! # fn example(mut conn: SqliteConnection) -> QueryResult<()> {
//! // Insert a row using `UIntBlob<u64>`
//! let row = DemoRow {
//!     id: 1,
//!     counter: UIntBlob(1234567890_u64),
//! };
//! diesel::insert_into(demo_table::table)
//!     .values(&row)
//!     .execute(&mut conn)?;
//!
//! // Fetch it back
//! let fetched: DemoRow = demo_table::table.find(1).first(&mut conn)?;
//! assert_eq!(fetched.counter.get(), 1234567890_u64);
//!
//! Ok(())
//! }
//! ```
//!
//! In this way, `UIntBlob<T>` gives you safe, compact serialisation
//! and correct numerical ordering of unsigned integers in SQLite
//! without resorting to application-level byte handling.
use std::convert::TryFrom;

use diesel::{
    backend::Backend,
    deserialize::{self, FromSql, FromSqlRow},
    expression::AsExpression,
    row::Row,
    serialize::{self, IsNull, Output, ToSql},
    sql_types::Binary,
    sqlite::Sqlite,
};
use serde::{Deserialize, Serialize};

/// Private module to "seal" the trait so only our chosen types
/// can implement it. Otherwise, users could implement it for
/// arbitrary types and defeat our goal of restricting `T`.
mod sealed {
    pub trait Sealed {}
    impl Sealed for u8 {}
    impl Sealed for u16 {}
    impl Sealed for u32 {}
    impl Sealed for u64 {}
    impl Sealed for u128 {}
}

/// This trait identifies the built-in unsigned types we allow. The
/// `NUM_BYTES` constant is used to pick the correct slice length.
pub trait ByteSizedUnsigned: sealed::Sealed + Copy + Into<u128> + TryFrom<u128> {
    const NUM_BYTES: usize;
}

impl ByteSizedUnsigned for u8 {
    const NUM_BYTES: usize = 1;
}

impl ByteSizedUnsigned for u16 {
    const NUM_BYTES: usize = 2;
}

impl ByteSizedUnsigned for u32 {
    const NUM_BYTES: usize = 4;
}

impl ByteSizedUnsigned for u64 {
    const NUM_BYTES: usize = 8;
}

impl ByteSizedUnsigned for u128 {
    const NUM_BYTES: usize = 16;
}

/// A typed wrapper storing an unsigned integer `T` in a BLOB using
/// big-endian, fixed-size serialisation.
///
/// For more details, see this module’s top-level documentation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct UIntBlob<T>(pub T);

/// A convenience alias for `UIntBlob<u8>`, storing a one-byte
/// unsigned integer (`u8`) as a big-endian BLOB.
pub type U8Blob = UIntBlob<u8>;

/// A convenience alias for `UIntBlob<u16>`, storing a two-byte
/// unsigned integer (`u16`) as a big-endian BLOB.
pub type U16Blob = UIntBlob<u16>;

/// A convenience alias for `UIntBlob<u32>`, storing a four-byte
/// unsigned integer (`u32`) as a big-endian BLOB.
pub type U32Blob = UIntBlob<u32>;

/// A convenience alias for `UIntBlob<u64>`, storing an eight-byte
/// unsigned integer (`u64`) as a big-endian BLOB.
pub type U64Blob = UIntBlob<u64>;

/// A convenience alias for `UIntBlob<u128>`, storing a sixteen-byte
/// unsigned integer (`u128`) as a big-endian BLOB.
pub type U128Blob = UIntBlob<u128>;

/// Implements `From<T>` so that you can directly create a
/// `UIntBlob<T>` from any plain integer `T`.
///
/// For example:
/// ```
/// use bpfman::uintblob::UIntBlob;
///
/// let blob_u32 = UIntBlob::from(42u32);
/// let blob_u64: UIntBlob<u64> = 42_u64.into();
/// ```
///
/// This is primarily a convenience method, sparing you from having to
/// write `UIntBlob(some_value)` explicitly.
impl<T> From<T> for UIntBlob<T>
where
    T: Copy,
{
    fn from(value: T) -> Self {
        UIntBlob(value)
    }
}

impl<T> UIntBlob<T>
where
    T: Copy,
{
    /// Returns a copy of the inner value `T`.
    ///
    /// This leaves the `UIntBlob` intact. If you want to consume it
    /// entirely and obtain the inner value once and for all, see
    /// [`into_inner`](Self::into_inner).
    pub fn get(&self) -> T {
        self.0
    }

    /// Consumes the `UIntBlob`, returning the inner value `T`.
    ///
    /// After calling `into_inner`, you cannot use the original
    /// `UIntBlob` anymore. This is useful in contexts where you only
    /// need the raw integer and do not plan to keep the wrapper.
    ///
    /// # Example
    ///
    /// ```
    /// use bpfman::uintblob::UIntBlob;
    ///
    /// let blob = UIntBlob(42u32);
    /// let value = blob.into_inner();
    /// assert_eq!(value, 42u32);
    /// // blob is now consumed and can no longer be used.
    /// ```
    pub fn into_inner(self) -> T {
        self.0
    }
}

/// A wrapper type for converting an unsigned integer into a
/// fixed-size big-endian byte representation. This preserves the
/// exact bit-width, ensures big-endian ordering in storage, and
/// allows safe serialisation and deserialisation of unsigned integer
/// types.
impl<T> UIntBlob<T>
where
    T: ByteSizedUnsigned,
{
    /// Converts the inner unsigned integer into a fixed-size
    /// big-endian byte vector. The result will be exactly
    /// `T::NUM_BYTES` bytes long, matching the byte-width of `T`.
    ///
    /// Internally, this method uses a 16-byte `u128` buffer, then
    /// extracts the trailing slice needed for `T`. Because of
    /// big-endian encoding, lexicographical comparisons (like
    /// `memcmp` in SQLite) match numerical order.
    fn to_bytes(self) -> Vec<u8> {
        // We get the 16-byte big-endian array from converting to
        // `u128`, then slice off only the trailing bytes relevant
        // to `T`.
        let full = self.0.into().to_be_bytes();
        let start = 16 - T::NUM_BYTES;
        full[start..].to_vec()
    }

    /// Constructs a `UIntBlob<T>` from a big-endian byte slice that
    /// must be exactly `T::NUM_BYTES` long. Otherwise, returns an
    /// error indicating a size mismatch.
    ///
    /// Once the bytes are placed into a 16-byte buffer (zero-padded
    /// on the front), they are converted back into a `u128`, then
    /// downcast to `T`. If that conversion fails (e.g., custom
    /// newtype constraints), an error is returned.
    pub fn from_bytes(bytes: &[u8]) -> deserialize::Result<Self> {
        if bytes.len() != T::NUM_BYTES {
            return Err(format!(
                "Invalid input size: expected {} bytes for `{}`, got {}",
                T::NUM_BYTES,
                std::any::type_name::<T>(),
                bytes.len()
            )
            .into());
        }

        let mut buf = [0u8; 16];
        buf[16 - T::NUM_BYTES..].copy_from_slice(bytes);

        let as_u128 = u128::from_be_bytes(buf);
        let value = T::try_from(as_u128).map_err(|_| {
            format!(
                "Out-of-range: {} does not fit into type `{}`",
                as_u128,
                std::any::type_name::<T>()
            )
        })?;

        Ok(UIntBlob(value))
    }
}

/// Implements `ToSql<Binary, Sqlite>` so that `UIntBlob<T>` can be
/// stored in a SQLite BLOB column via Diesel. When inserting or
/// updating a `UIntBlob<T>` in a query, Diesel will invoke this
/// method to serialise the value.
///
/// This writes exactly `T::NUM_BYTES` bytes to the output in
/// big-endian order, ensuring SQLite will store it in a minimal,
/// lexicographically comparable form.
impl<T> ToSql<Binary, Sqlite> for UIntBlob<T>
where
    T: ByteSizedUnsigned + std::fmt::Debug,
    // The `u128: From<T>` bound is implied by `T: Into<u128>` but
    // spelled out commonly for clarity.
{
    fn to_sql<'b>(&'b self, out: &mut Output<'b, '_, Sqlite>) -> serialize::Result {
        out.set_value(self.to_bytes());
        Ok(IsNull::No)
    }
}

/// Implements `FromSql<Binary, Sqlite>` for `UIntBlob<T>`, allowing
/// Diesel to deserialise a SQLite BLOB column into a `UIntBlob<T>`.
///
/// When reading a `BLOB` from a query result, Diesel first retrieves
/// the raw bytes as a `Vec<u8>`. This implementation then passes
/// those bytes to [`from_bytes`](UIntBlob::from_bytes), verifying:
///
/// 1. They have the exact length for `T`.
/// 2. They can be safely converted to `T`.
///
/// If these checks fail, an error is returned rather than producing
/// an invalid integer value.
impl<T> FromSql<Binary, Sqlite> for UIntBlob<T>
where
    T: ByteSizedUnsigned + Copy,
{
    fn from_sql(bytes: <Sqlite as Backend>::RawValue<'_>) -> deserialize::Result<Self> {
        let blob = <Vec<u8> as FromSql<Binary, Sqlite>>::from_sql(bytes)?;
        Self::from_bytes(&blob)
    }
}

/// Allows an owned `UIntBlob<T>` to be used as a Diesel expression of
/// type `Binary`. This makes it possible to write queries like:
///
/// ```rust,ignore
/// use diesel::prelude::*;
/// use my_crate::uintblob::UIntBlob;
///
/// // Suppose `table::col` is a BLOB column
/// table.filter(table::col.eq(UIntBlob::from(123u64)))
///      .load::<MyRow>(&mut conn)?;
/// ```
///
/// Internally, the `UIntBlob<T>` is converted to a `Vec<u8>` in
/// big-endian form, preserving both numeric order and minimal size.
impl<T> AsExpression<Binary> for UIntBlob<T>
where
    T: ByteSizedUnsigned + std::fmt::Debug,
{
    type Expression = <Vec<u8> as AsExpression<Binary>>::Expression;

    fn as_expression(self) -> Self::Expression {
        <Vec<u8> as AsExpression<Binary>>::as_expression(self.to_bytes())
    }
}

/// Allows a reference to `UIntBlob<T>` (`&UIntBlob<T>`) to be used as
/// a Diesel expression of type `Binary`. This is particularly helpful
/// when you want to avoid consuming the `UIntBlob<T>` and you already
/// have a reference to it:
///
/// ```rust,ignore
/// let blob_value = UIntBlob::from(123u64);
/// // We can pass a reference into the query:
/// table.filter(table::col.eq(&blob_value))
///      .load::<MyRow>(&mut conn)?;
/// ```
///
/// Diesel treats this the same as owned `UIntBlob<T>` in queries,
/// converting it into a byte vector before binding.
impl<T> AsExpression<Binary> for &UIntBlob<T>
where
    T: ByteSizedUnsigned + std::fmt::Debug,
{
    type Expression = <Vec<u8> as AsExpression<Binary>>::Expression;

    fn as_expression(self) -> Self::Expression {
        self.to_owned().as_expression()
    }
}

/// Implements `FromSqlRow<Binary, Sqlite>` so Diesel can
/// automatically retrieve a `UIntBlob<T>` from the database without
/// needing low-level byte handling in user code. If this trait were
/// missing, you would have to query for `Vec<u8>` yourself and then
/// manually call [`UIntBlob::from_bytes`] on that `Vec<u8>`.
///
/// Under the hood, Diesel first gets the raw bytes as a `Vec<u8>`
/// from the BLOB column, then calls our
/// [`from_bytes`](UIntBlob::from_bytes) function. If the size doesn’t
/// match `T::NUM_BYTES`, or if `T::try_from(u128)` fails, the result
/// is an error rather than silently returning invalid data.
impl<T> FromSqlRow<Binary, Sqlite> for UIntBlob<T>
where
    T: ByteSizedUnsigned + Copy,
{
    fn build_from_row<'a>(row: &impl Row<'a, Sqlite>) -> deserialize::Result<Self> {
        let v: Vec<u8> = <Vec<u8> as FromSqlRow<Binary, Sqlite>>::build_from_row(row)?;
        Self::from_bytes(&v)
    }
}

#[cfg(test)]
mod tests {
    use std::fmt::Display;

    use diesel::{
        prelude::*,
        sql_types::{Binary, Text},
        sqlite::SqliteConnection,
    };

    use super::*;

    /// A generic test function that verifies correct insertion and
    /// retrieval of values for any type `T: ByteSizedUnsigned` by:
    ///
    /// 1. Creating an in-memory SQLite DB with a table that has
    ///    a `BLOB` column (`value`) and a `TEXT` column (`text_value`).
    ///
    /// 2. Inserting each test value twice:
    ///    - Once as a `UIntBlob<T>` into the `BLOB` column
    ///    - Once as the stringified form into the `TEXT` column
    ///
    /// 3. Sorting our input `values` in ascending order, then
    ///    retrieving them back from the database ordered by `value`.
    ///    Because the BLOB is stored in big-endian format,
    ///    lexicographical ordering should match numeric ordering.
    ///
    /// 4. Comparing both the deserialised blob data and the stored
    ///    text to confirm they match the original `values`.
    ///
    /// If any step fails (e.g. if the ordering is incorrect or if the
    /// text/value mismatch), the test will panic, indicating a
    /// regression in `UIntBlob<T>` handling.
    fn run_blob_test<T>(mut values: Vec<T>)
    where
        T: ByteSizedUnsigned + PartialEq + std::fmt::Debug + Display + Ord + 'static,
        <T as TryFrom<u128>>::Error: std::fmt::Debug,
    {
        let mut conn = SqliteConnection::establish(":memory:").unwrap();

        diesel::sql_query(
            "CREATE TABLE test_blobs (value BLOB NOT NULL, text_value TEXT NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();

        for &val in &values {
            diesel::sql_query("INSERT INTO test_blobs (value, text_value) VALUES (?, ?)")
                .bind::<Binary, _>(UIntBlob::from(val))
                .bind::<Text, _>(val.to_string())
                .execute(&mut conn)
                .unwrap();
        }

        #[derive(QueryableByName)]
        struct BlobRow<T> {
            #[diesel(sql_type = Binary)]
            value: UIntBlob<T>,
            #[diesel(sql_type = Text)]
            text_value: String,
        }

        let rows: Vec<BlobRow<T>> =
            diesel::sql_query("SELECT value, text_value FROM test_blobs ORDER BY value ASC")
                .load(&mut conn)
                .unwrap();

        values.sort();
        let expected_text: Vec<String> = values.iter().map(|v| v.to_string()).collect();
        let retrieved_values: Vec<T> = rows.iter().map(|r| r.value.get()).collect();
        let retrieved_text: Vec<String> = rows.iter().map(|r| r.text_value.clone()).collect();

        assert_eq!(retrieved_values, values);
        assert_eq!(retrieved_text, expected_text);
    }

    #[test]
    fn test_blob_for_all_types() {
        run_blob_test::<u8>(vec![0, 42, u8::MAX, 1, 255]);
        run_blob_test::<u16>(vec![0, 42, u16::MAX, 1, 999]);
        run_blob_test::<u32>(vec![0, 42, u32::MAX, 1, 1000]);
        run_blob_test::<u64>(vec![0, 42, u64::MAX, 1, 5000]);
        run_blob_test::<u128>(vec![0, 42, u128::MAX, 1, 12345678901234567890u128]);
    }

    /// Verifies that inserting a physically undersized BLOB (1 byte)
    /// and then attempting to deserialise it as a 2-byte
    /// `UIntBlob<u16>` fails.
    ///
    /// # Why this differs from `test_mismatched_uintblob_size_should_fail`
    ///
    /// - That other test inserts a valid 8-byte BLOB for
    ///   `UIntBlob<u64>` but tries to read it as `UIntBlob<u16>`. The
    ///   stored data is correct for `u64`, but we request it under
    ///   the wrong type.
    ///
    /// - Here, we insert a 1-byte BLOB that is never correct for
    ///   `u16` (which needs 2 bytes). The BLOB itself is physically
    ///   invalid for that type, so the failure is due to an outright
    ///   length mismatch, not a type mismatch on valid data.
    ///
    /// # Overview
    ///
    /// - We create a table with a BLOB column.
    ///
    /// - Insert a single byte (`42u8`) into that column.
    ///
    /// - Attempt to query the row as `UIntBlob<u16>` (2 bytes).
    ///
    /// - Since the stored length does not match `u16`'s expected
    ///   size, we get an error instead of producing a truncated or
    ///   invalid integer.
    #[test]
    fn test_invalid_blob_length() {
        let mut conn = SqliteConnection::establish(":memory:").unwrap();

        diesel::sql_query(
            "CREATE TABLE test_blobs (value BLOB NOT NULL, text_value TEXT NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();

        // For u16, we expect a 2-byte BLOB. Insert a BLOB of 1 byte.
        diesel::sql_query("INSERT INTO test_blobs (value, text_value) VALUES (?, ?)")
            .bind::<Binary, _>(vec![42u8])
            .bind::<Text, _>("invalid")
            .execute(&mut conn)
            .unwrap();

        // This struct exists solely to enable Diesel to deserialize
        // the query result via QueryableByName. In this test,
        // deserialisation is expected to fail.
        #[allow(dead_code)]
        #[derive(QueryableByName)]
        struct BlobRow {
            #[diesel(sql_type = Binary)]
            value: UIntBlob<u16>,
            #[diesel(sql_type = Text)]
            text_value: String,
        }

        let result: Result<BlobRow, _> =
            diesel::sql_query("SELECT value, text_value FROM test_blobs").get_result(&mut conn);
        assert!(result.is_err(), "Expected error due to invalid blob length");
    }

    // --- Diesel Integration Tests ---

    // Define a simple table `numbers` with an INTEGER primary key and a BLOB column.
    table! {
        numbers (id) {
            id -> Integer,
            value -> Binary,
        }
    }

    /// Demonstrates that `UIntBlob<T>` works properly with Diesel’s
    /// `Identifiable`, `Insertable`, and `Queryable` traits and shows
    /// how type mismatches behave in filters:
    ///
    /// 1. Identifiable & Insertable: We create a struct with
    ///    `#[derive(Identifiable, Insertable, Queryable)]` and a
    ///    `UIntBlob<u16>` field, inserting multiple rows into a `BLOB
    ///    NOT NULL` column.
    ///
    /// 2. Retrieval with `find`: We verify that `numbers::table
    ///    .find(1).first()` loads the correct row if `value` matches
    ///    the expected size and type (`u16` in this case).
    ///
    /// 3. Type mismatch in a filter: Filtering with
    ///    `UIntBlob<u32>` does not find a match, showing that
    ///    mismatched sizes lead to no results (rather than silently
    ///    matching).
    ///
    /// 4. Correct type in a filter: We confirm that matching on
    ///    the actual `u16` size retrieves the expected row without
    ///    errors.
    #[test]
    fn test_identifiable() {
        #[derive(Identifiable, Insertable, Queryable)]
        #[diesel(table_name = numbers)]
        struct NumberRow {
            id: i32,
            value: UIntBlob<u16>,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        let rows = vec![
            NumberRow {
                id: 1,
                value: 42.into(),
            },
            NumberRow {
                id: 2,
                value: 43.into(),
            },
        ];

        diesel::insert_into(numbers::table)
            .values(&rows)
            .execute(&mut conn)
            .unwrap();

        {
            // Find with correct type.
            let found = numbers::table
                .find(1)
                .first::<NumberRow>(&mut conn)
                .unwrap();
            assert_eq!(found.value.0, 42u16);
        }

        {
            // Search with mismatched type should find nothing.
            let not_found = numbers::table
                .filter(numbers::value.eq(UIntBlob::from(43u32)))
                .first::<NumberRow>(&mut conn);
            assert!(not_found.is_err());
        }

        {
            // Search with correct type finds correct row.
            let found = numbers::table
                .filter(numbers::value.eq(UIntBlob::from(43u16)))
                .first::<NumberRow>(&mut conn)
                .unwrap();
            assert_eq!(found.id, 2);
        }
    }

    /// Checks that multiple rows sharing the same `UIntBlob<u16>`
    /// value can be deleted in one query, demonstrating how equality
    /// comparison works with `UIntBlob<T>` in Diesel filters:
    ///
    /// 1. Inserts three rows, where two have `value = 43u16` and one
    ///    has `value = 44u16`.
    /// 2. Executes a `DELETE` statement filtering on
    ///    `numbers::value.eq(UIntBlob(43u16))`.
    /// 3. Verifies that exactly the two matching rows were deleted,
    ///    leaving only one row behind.
    #[test]
    fn test_delete_by_blob_value() {
        #[derive(Insertable, Queryable, Debug, PartialEq)]
        #[diesel(table_name = numbers)]
        struct NumberRow {
            id: i32,
            value: UIntBlob<u16>,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        let rows = vec![
            NumberRow {
                id: 1,
                value: 1u16.into(),
            },
            NumberRow {
                id: 2,
                value: 43u16.into(),
            },
            NumberRow {
                id: 3,
                value: 44u16.into(),
            },
        ];

        diesel::insert_into(numbers::table)
            .values(&rows)
            .execute(&mut conn)
            .unwrap();

        {
            // nothing should match.
            let deleted =
                diesel::delete(numbers::table.filter(numbers::value.eq(UIntBlob::from(43u32))))
                    .execute(&mut conn)
                    .unwrap();
            assert_eq!(deleted, 0);

            let remaining: Vec<NumberRow> = numbers::table.load(&mut conn).unwrap();
            assert_eq!(
                remaining.len(),
                3,
                "Unexpected row deletion before correct delete!"
            );
        }

        {
            // delete matched row.
            let remaining: Vec<NumberRow> = numbers::table.load(&mut conn).unwrap();
            assert_eq!(remaining.len(), 3);

            let deleted =
                diesel::delete(numbers::table.filter(numbers::value.eq(UIntBlob::from(43u16))))
                    .execute(&mut conn)
                    .unwrap();
            assert_eq!(deleted, 1);

            let remaining: Vec<NumberRow> = numbers::table.load(&mut conn).unwrap();
            assert_eq!(remaining.len(), 2);
            assert_eq!(remaining[0].value.get(), 1u16);
            assert_eq!(remaining[1].value.get(), 44u16);
        }
    }

    /// Verifies that multiple rows with the same `UIntBlob<u16>`
    /// value can be deleted in a single Diesel `delete` operation.
    /// Specifically:
    ///
    /// 1. We insert three rows into the database, two of which have
    ///    `value = 43u16`.
    //
    /// 2. We execute `DELETE ... WHERE value = 43u16`, expecting
    ///    exactly two rows to be removed.
    ///
    /// 3. We confirm that the remaining row still has a distinct
    ///    value (`44u16`), ensuring the filter matched only the
    ///    intended rows.
    #[test]
    fn test_delete_multiple_matching_blob_values() {
        #[derive(Insertable, Queryable, Debug, PartialEq)]
        #[diesel(table_name = numbers)]
        struct NumberRow {
            id: i32,
            value: UIntBlob<u16>,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        let rows = vec![
            NumberRow {
                id: 1,
                value: 43u16.into(),
            },
            NumberRow {
                id: 2,
                value: 43u16.into(),
            },
            NumberRow {
                id: 3,
                value: 44u16.into(),
            },
        ];

        diesel::insert_into(numbers::table)
            .values(&rows)
            .execute(&mut conn)
            .unwrap();

        {
            // Delete *all* rows where value == 43u16.
            let deleted =
                diesel::delete(numbers::table.filter(numbers::value.eq(UIntBlob::from(43u16))))
                    .execute(&mut conn)
                    .unwrap();
            assert_eq!(deleted, 2, "Expected 2 rows to be deleted!");

            // Verify only 1 row remains.
            let remaining: Vec<NumberRow> = numbers::table.load(&mut conn).unwrap();
            assert_eq!(remaining.len(), 1);
            assert_eq!(remaining[0].value.get(), 44u16);
        }
    }

    /// Demonstrates that updating a SQLite BLOB with a mismatched
    /// `UIntBlob<T>` type does not fail at the database level, but
    /// fails when attempting to retrieve the value later.
    ///
    /// # Why This Differs from `test_mismatched_uintblob_size_should_fail`
    ///
    /// - That earlier test shows a mismatch during retrieval only (no
    ///   update is done).
    ///
    /// - This test updates an existing row to a different integer
    ///   type. SQLite, lacking size enforcement on BLOB columns, does
    ///   not complain. But deserialising again as `UIntBlob<u16>`
    ///   fails.
    ///
    /// # What This Proves
    ///
    /// 1. Create (Insert) works correctly with `UIntBlob<T>`.
    ///
    /// 2. Read (Retrieve) succeeds as long as the stored type
    ///    matches the expected type.
    ///
    /// 3. Update with the wrong type does not error out
    ///    immediately, but retrieval will fail, preventing silent
    ///    data corruption.
    ///
    /// 4. AsChangeset trait usage is confirmed by successfully
    ///    updating the row with both a mismatched type (triggering an
    ///    error on read) and the correct type (restoring normal
    ///    functionality).
    #[test]
    fn test_update() {
        table! {
            numbers (id) {
                id -> Integer,
                value -> Binary, // No type enforcement at DB level.
            }
        }

        #[derive(Identifiable, Insertable, Queryable, AsChangeset, Debug, PartialEq)]
        #[diesel(table_name = numbers)]
        struct NumberRow {
            id: i32,
            // This test assumes `value` should only store `u16`.
            value: UIntBlob<u16>,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        let initial = NumberRow {
            id: 1,
            value: 42.into(),
        };

        // Step 1: Verify Insertable Trait (Create).
        diesel::insert_into(numbers::table)
            .values(&initial)
            .execute(&mut conn)
            .unwrap();

        // Step 2: Verify Read (Retrieve).
        let retrieved: NumberRow = numbers::table.find(1).first(&mut conn).unwrap();
        assert_eq!(
            retrieved.value.get(),
            42u16,
            "Unexpected retrieval failure after insert!"
        );

        {
            // Step 3: Attempt to update with a different type
            // (`UIntBlob<u32>`).
            let update_result = diesel::update(numbers::table.find(1))
                .set(numbers::value.eq(UIntBlob::from(43u32))) // Wrong type (u32 instead of u16)
                .execute(&mut conn);

            // SQLite allows the update because `BLOB` has no built-in
            // size enforcement.
            assert!(
                update_result.is_ok(),
                "Unexpected failure during the update!"
            );

            // Step 4: Try to retrieve it back as `UIntBlob<u16>`.
            let retrieval_result = numbers::table.find(1).first::<NumberRow>(&mut conn);

            match retrieval_result {
                Ok(row) => {
                    panic!(
                        "Expected retrieval failure due to mismatched size, but got value: {:?}",
                        row
                    );
                }
                Err(err) => {
                    // Expected failure due to
                    // `UIntBlob<u16>::from_bytes()` rejecting the
                    // size mismatch.
                    println!("Expected retrieval error occurred: {}", err);
                }
            }
        }

        {
            // Step 5: Verify Update Semantics (AsChangeset Trait)
            // Update again using the correct type (u16), which should
            // succeed.
            diesel::update(numbers::table.find(1))
                .set(numbers::value.eq(UIntBlob::from(44u16))) // Correct type (u16).
                .execute(&mut conn)
                .unwrap();

            let updated: NumberRow = numbers::table.find(1).first(&mut conn).unwrap();
            assert_eq!(
                updated.value.get(),
                44u16,
                "Unexpected retrieval failure after correct update!"
            );
        }
    }

    /// Verifies that a user-defined table column of type `Binary` can
    /// directly map to a `U64Blob` (i.e., `UIntBlob<u64>`), showing
    /// that our wrapper type works as a normal Diesel field.
    ///
    /// We create a table `custom_numbers` with a BLOB column, define
    /// a struct that includes a `U64Blob` for that column, then:
    ///
    /// 1. Insert a row using `u64::MAX.into()`
    /// 2. Retrieve it back to ensure the value remains intact.
    /// 3. Confirm that the round-trip was successful by matching
    ///    `retrieved.value` against `u64::MAX`.
    #[test]
    fn test_custom_uintblob_column_type() {
        use diesel::prelude::*;

        table! {
            custom_numbers (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Insertable, Queryable, Debug, PartialEq)]
        #[diesel(table_name = custom_numbers)]
        struct CustomNumberRow {
            id: i32,
            value: U64Blob,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query(
            "CREATE TABLE custom_numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();

        let row = CustomNumberRow {
            id: 1,
            value: u64::MAX.into(),
        };

        diesel::insert_into(custom_numbers::table)
            .values(&row)
            .execute(&mut conn)
            .unwrap();

        let retrieved: CustomNumberRow = custom_numbers::table.find(1).first(&mut conn).unwrap();
        assert_eq!(retrieved.value.get(), u64::MAX);
    }

    /// This test verifies that attempting to deserialise a stored
    /// `UIntBlob<u64>` as a `UIntBlob<u16>` fails at runtime.
    ///
    /// # Why This Test Exists
    ///
    /// - SQLite does not enforce data sizes in a `BLOB` column. You
    ///   can store any length of bytes there.
    ///
    /// - `UIntBlob<T>` enforces that its byte slice must be exactly
    ///   `T::NUM_BYTES`.
    ///
    /// - Hence, if we insert a `UIntBlob<u64>` (8 bytes) but later
    ///   try to retrieve it as a `UIntBlob<u16>` (2 bytes), the
    ///   deserialisation should fail. This is a subtle programmer
    ///   error.
    ///
    /// # How the Failure is Detected
    ///
    /// - During retrieval, Diesel calls `FromSql<Binary, Sqlite>` on
    ///   `UIntBlob<u16>`, which invokes
    ///   [`UIntBlob::from_bytes`](crate::uintblob::UIntBlob::from_bytes).
    ///
    /// - `from_bytes` checks that `bytes.len() == T::NUM_BYTES`. If
    ///   the stored BLOB has 8 bytes but we're expecting 2, it
    ///   returns an error immediately rather than silently
    ///   truncating.
    ///
    /// # Key Point: No Data Corruption
    ///
    /// - The database still holds the correct 8-byte data for `u64`.
    /// - We simply cannot read it back as a `u16`.
    /// - This protects us from mismatched types by failing fast.
    ///
    /// # Expected Outcome
    ///
    /// - Diesel should raise an error on mismatch, so the test passes
    ///   if an error occurs.
    #[test]
    fn test_mismatched_uintblob_size_should_fail() {
        use diesel::prelude::*;

        table! {
            numbers (id) {
                id -> Integer,
                value -> Binary, // No size enforcement at DB level
            }
        }

        #[derive(Insertable, Queryable, Debug, PartialEq)]
        #[diesel(table_name = numbers)]
        struct NumberRowU64 {
            id: i32,
            value: U64Blob, // UIntBlob<u64>
        }

        #[derive(Queryable, Debug)]
        #[diesel(table_name = numbers)]
        struct NumberRowU16 {
            id: i32,
            value: U16Blob, // UIntBlob<u16> (wrong type when reading)
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        // Insert a `UIntBlob<u64>` (8-byte value)
        let row = NumberRowU64 {
            id: 1,
            value: u64::MAX.into(),
        };

        diesel::insert_into(numbers::table)
            .values(&row)
            .execute(&mut conn)
            .unwrap();

        // Now attempt to read it as a `UIntBlob<u16>` (expect
        // failure). Note: this is a subtle runtime failure.
        let result = numbers::table.find(1).first::<NumberRowU16>(&mut conn);

        match result {
            Ok(row) => {
                println!("Unexpected success: id={} value={:?}", row.id, row.value);
                panic!("Expected deserialisation failure, but got a row!");
            }
            Err(err) => {
                println!("Expected error occurred: {}", err);
            }
        }
    }

    /// Tests that a `UIntBlob<u64>` can round-trip through both
    /// SQLite and JSON serialisation, verifying that:
    ///
    /// 1. When inserted and retrieved from the `serialized_numbers`
    ///    table, the row matches the original (`DB retrieval
    ///    mismatch` check).
    ///
    /// 2. Serialising to JSON uses an integer representation (not a
    ///    blob or string), as evidenced by comparing it to
    ///    `expected_json`.
    ///
    /// 3. Deserialising the JSON back into `NumberRow` yields the
    ///    same values, confirming a complete in-memory → DB → J
    #[test]
    fn test_uintblob_serialisation_round_trip() {
        use diesel::prelude::*;
        use serde_json;

        table! {
            serialized_numbers (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Insertable, Queryable, Debug, PartialEq, Serialize, Deserialize)]
        #[diesel(table_name = serialized_numbers)]
        struct NumberRow {
            id: i32,
            value: U64Blob, // UIntBlob<u64>
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query(
            "CREATE TABLE serialized_numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();

        let original_row = NumberRow {
            id: 1,
            value: u64::MAX.into(),
        };

        diesel::insert_into(serialized_numbers::table)
            .values(&original_row)
            .execute(&mut conn)
            .unwrap();

        let retrieved_row: NumberRow = serialized_numbers::table.find(1).first(&mut conn).unwrap();
        assert_eq!(retrieved_row, original_row, "DB retrieval mismatch!");

        let json = serde_json::to_string(&retrieved_row).unwrap();
        println!("Serialized JSON: {}", json);

        let expected_json = format!(r#"{{"id":1,"value":{}}}"#, u64::MAX);
        assert_eq!(json, expected_json, "JSON serialisation mismatch!");

        let deserialized_row: NumberRow = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized_row, retrieved_row, "JSON round-trip mismatch!");
    }

    /// Tests retrieving rows by BLOB value, including edge cases with
    /// mismatched types.
    ///
    /// ### Type Mismatches in BLOB Comparisons
    ///
    /// A key observation in this test is how SQLite handles BLOBs
    /// with different sizes when used in a query:
    ///
    /// - When we search using a `UIntBlob<u32>` in a table containing
    ///   `UIntBlob<u16>` values, we get an empty result set rather
    ///   than an error.
    ///
    /// - This behaviour occurs because:
    ///   1. `UIntBlob<u32>` serializes to a 4-byte BLOB
    ///   2. `UIntBlob<u16>` serializes to a 2-byte BLOB
    ///   3. SQLite compares BLOBs using memcmp
    ///   4. A 4-byte value will never match a 2-byte value
    ///
    /// - While our deserialisation code throws errors when trying to
    ///   read mismatched BLOB sizes, the SQLite query itself simply
    ///   finds no matches.
    ///
    /// This test verifies that our abstractions correctly handle both
    /// matched and mismatched type scenarios during queries.
    #[test]
    fn test_find_by_blob_value() {
        use diesel::prelude::*;

        table! {
            numbers (id) {
                id -> Integer,
                value -> Binary,
            }
        }

        #[derive(Insertable, Queryable, Debug, PartialEq)]
        #[diesel(table_name = numbers)]
        struct NumberRow {
            id: i32,
            value: UIntBlob<u16>,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query("CREATE TABLE numbers (id INTEGER PRIMARY KEY, value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        // Insert multiple rows with different values
        let rows = vec![
            NumberRow {
                id: 1,
                value: 42u16.into(),
            },
            NumberRow {
                id: 2,
                value: 43u16.into(),
            },
            NumberRow {
                id: 3,
                value: 44u16.into(),
            },
            NumberRow {
                id: 4,
                value: 42u16.into(), // Duplicate value to verify multiple matches
            },
        ];

        diesel::insert_into(numbers::table)
            .values(&rows)
            .execute(&mut conn)
            .unwrap();

        // Test finding rows by blob value
        let found_rows: Vec<NumberRow> = numbers::table
            .filter(numbers::value.eq(UIntBlob::from(42u16)))
            .load(&mut conn)
            .unwrap();

        assert_eq!(found_rows.len(), 2, "Expected to find 2 rows with value 42");
        assert!(found_rows.iter().all(|row| row.value.get() == 42u16));
        assert!(found_rows.iter().any(|row| row.id == 1));
        assert!(found_rows.iter().any(|row| row.id == 4));

        // Test finding with a value that doesn't exist
        let not_found: Vec<NumberRow> = numbers::table
            .filter(numbers::value.eq(UIntBlob::from(99u16)))
            .load(&mut conn)
            .unwrap();

        assert_eq!(not_found.len(), 0, "Expected to find 0 rows with value 99");

        // Test finding with mismatched type (u32 instead of u16)
        // This is a key test for understanding BLOB comparisons:
        // - SQLite finds matches using binary comparison (like memcmp)
        // - A u32 value (4 bytes) will never match a u16 value (2 bytes)
        // - The query executes successfully but returns no matches
        // - No type error occurs at the query level - SQLite has no concept of integer types in BLOBs
        let mismatched_type_rows: Vec<NumberRow> = numbers::table
            .filter(numbers::value.eq(UIntBlob::from(42u32)))
            .load(&mut conn)
            .unwrap_or_else(|e| {
                panic!(
                    "Expected query to succeed with no results, but got error: {}",
                    e
                );
            });

        assert_eq!(
            mismatched_type_rows.len(),
            0,
            "Searching with mismatched type should return no rows, not an error"
        );
    }

    /// Verifies that ordering of `UIntBlob<u128>` in a SQLite BLOB
    /// column matches numeric expectations when using SQL's `ORDER
    /// BY` clauses.
    ///
    /// We insert a variety of `u128` values, spanning boundary cases
    /// for 8-, 16-, 32-, 64-, and 128-bit integers. We then query
    /// them back in ascending and descending order to ensure:
    ///
    /// 1. Big-endian storage truly yields correct lexicographical
    ///    ordering that aligns with numeric ordering.
    ///
    /// 2. Boundary and random values sort as expected, covering
    ///    interesting bit-patterns as well as minimums, maximums, and
    ///    mid-range values.
    #[test]
    fn test_uintblob_ordering() {
        let mut conn = SqliteConnection::establish(":memory:").unwrap();

        // Create a simple table with a BLOB value.
        diesel::sql_query("CREATE TABLE blob_test (value BLOB PRIMARY KEY)")
            .execute(&mut conn)
            .unwrap();

        // These values are chosen to test ordering at critical
        // boundary thresholds.
        let values = vec![
            0u128, // Zero
            1u128, // One
            // 8-bit boundaries (u8).
            127u128, // 2^7 - 1
            128u128, // 2^7
            255u128, // 2^8 - 1
            256u128, // 2^8
            // 16-bit boundaries (u16).
            32767u128, // 2^15 - 1
            32768u128, // 2^15
            65535u128, // 2^16 - 1
            65536u128, // 2^16
            // 32-bit boundaries (u32).
            2147483647u128, // 2^31 - 1
            2147483648u128, // 2^31
            4294967295u128, // 2^32 - 1
            4294967296u128, // 2^32
            // 64-bit boundaries (u64).
            9223372036854775807u128,  // 2^63 - 1
            9223372036854775808u128,  // 2^63
            18446744073709551615u128, // 2^64 - 1
            18446744073709551616u128, // 2^64
            // 128-bit values.
            170141183460469231731687303715884105727u128, // 2^127 - 1
            170141183460469231731687303715884105728u128, // 2^127
            340282366920938463463374607431768211455u128, // 2^128 - 1 (u128::MAX)
            // Random values with interesting bit patterns.
            42u128,
            12345678901234567890u128,
            0xDEADBEEFu128,
            0x0123456789ABCDEFu128,
            0xFEDCBA9876543210u128,
        ];

        // Define a struct to receive query results.
        #[derive(QueryableByName, Debug)]
        struct BlobRow {
            #[diesel(sql_type = Binary)]
            value: UIntBlob<u128>,
        }

        // Insert all values.
        for val in &values {
            diesel::sql_query("INSERT INTO blob_test (value) VALUES (?)")
                .bind::<diesel::sql_types::Binary, _>(UIntBlob::from(*val))
                .execute(&mut conn)
                .unwrap();
        }

        // Expected values in ascending order.
        let mut expected_asc = values.clone();
        expected_asc.sort();

        // Query with ORDER BY ascending
        let results: Vec<BlobRow> =
            diesel::sql_query("SELECT value FROM blob_test ORDER BY value ASC")
                .load(&mut conn)
                .unwrap();

        // Extract values and compare.
        let result_values: Vec<u128> = results.iter().map(|row| row.value.get()).collect();

        assert_eq!(result_values, expected_asc, "Ascending order incorrect");

        // Expected values in descending order.
        let mut expected_desc = values.clone();
        expected_desc.sort_by(|a, b| b.cmp(a));

        // Query with ORDER BY descending.
        let results: Vec<BlobRow> =
            diesel::sql_query("SELECT value FROM blob_test ORDER BY value DESC")
                .load(&mut conn)
                .unwrap();

        // Extract values and compare
        let result_values: Vec<u128> = results.iter().map(|row| row.value.get()).collect();

        assert_eq!(result_values, expected_desc, "Descending order incorrect");
    }

    /// Verifies that SQLite's aggregate functions behave correctly
    /// with `UIntBlob<T>` stored in a BLOB column. In particular, we
    /// test:
    ///
    /// 1. `MIN(value)` and `MAX(value)`:
    ///    - Because we store unsigned integers in big-endian format,
    ///      lexicographical ordering in SQLite matches numeric
    ///      ordering. Thus, `MIN` and `MAX` should return the
    ///      numerically smallest and largest values.
    ///
    /// 2. `COUNT(value)`:
    ///    - Ensures we can count rows without any issue when using
    ///      `UIntBlob<T>` in a BLOB column.
    ///    - Also checks a WHERE clause to confirm binary comparisons
    ///      filter rows as expected.
    ///
    /// Note that we do *not* test `AVG` or other arithmetic
    /// functions. SQLite treats BLOBs as opaque binary data, so there
    /// is no numerical interpretation for averaging. While our
    /// big-endian format allows correct lexicographical comparisons,
    /// it does not convert BLOBs into meaningful numbers for
    /// arithmetic computations.
    #[test]
    fn test_uintblob_aggregates() {
        use diesel::sql_types::BigInt;

        #[derive(QueryableByName, Debug)]
        struct MinMaxResult {
            #[diesel(sql_type = Binary)]
            value: UIntBlob<u32>,
        }

        #[derive(QueryableByName, Debug)]
        struct CountResult {
            #[diesel(sql_type = BigInt)]
            count: i64,
        }

        let mut conn = SqliteConnection::establish(":memory:").unwrap();

        diesel::sql_query("CREATE TABLE blob_aggregates (value BLOB NOT NULL)")
            .execute(&mut conn)
            .unwrap();

        // Must be ordered low -> high.
        let values = vec![
            0u32,
            42u32,
            100u32,
            255u32,
            1000u32,
            10000u32,
            100000u32,
            u32::MAX,
        ];

        for val in &values {
            diesel::sql_query("INSERT INTO blob_aggregates (value) VALUES (?)")
                .bind::<diesel::sql_types::Binary, _>(UIntBlob::from(*val))
                .execute(&mut conn)
                .unwrap();
        }

        // Test MIN aggregate function.
        let min_result: MinMaxResult =
            diesel::sql_query("SELECT MIN(value) as value FROM blob_aggregates")
                .get_result(&mut conn)
                .unwrap();

        assert_eq!(
            min_result.value.get(),
            0u32,
            "MIN function returned incorrect result"
        );

        // Test MAX aggregate function.
        let max_result: MinMaxResult =
            diesel::sql_query("SELECT MAX(value) as value FROM blob_aggregates")
                .get_result(&mut conn)
                .unwrap();

        assert_eq!(
            max_result.value.get(),
            u32::MAX,
            "MAX function returned incorrect result"
        );

        // Test COUNT aggregation function.
        let count_result: CountResult =
            diesel::sql_query("SELECT COUNT(value) as count FROM blob_aggregates")
                .get_result(&mut conn)
                .unwrap();

        assert_eq!(
            count_result.count,
            values.len() as i64,
            "COUNT function returned incorrect result"
        );

        // Test COUNT aggregation with a WHERE clause.
        let count_filtered: CountResult =
            diesel::sql_query("SELECT COUNT(value) as count FROM blob_aggregates WHERE value > ?")
                .bind::<diesel::sql_types::Binary, _>(UIntBlob(100u32))
                .get_result(&mut conn)
                .unwrap();

        let expected_count = values.iter().filter(|&&v| v > 100).count() as i64;
        assert_eq!(
            count_filtered.count, expected_count,
            "COUNT with WHERE clause returned incorrect result"
        );
    }
}
