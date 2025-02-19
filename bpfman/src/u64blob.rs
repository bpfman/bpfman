// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::io;

use diesel::{
    backend::Backend,
    deserialize::{self, FromSql},
    serialize::{self, IsNull, Output, ToSql},
    sql_types::Binary,
    sqlite::Sqlite,
};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct U64Blob(pub u64);

const ERROR_EXPECTED_BLOB: &str = "Expected blob";
const ERROR_INCORRECT_LENGTH: &str = "Incorrect length (expected 8 bytes)";
const ERROR_CONVERSION_FAILED: &str = "Failed to convert blob to u64";

impl ToSql<Binary, Sqlite> for U64Blob {
    fn to_sql<'b>(&'b self, out: &mut Output<'b, '_, Sqlite>) -> serialize::Result {
        // Convert directly to a Vec<u8> which has owned data.
        let bytes: Vec<u8> = self.0.to_be_bytes().to_vec();
        out.set_value(bytes);
        Ok(IsNull::No)
    }
}

impl FromSql<Binary, Sqlite> for U64Blob {
    fn from_sql(bytes: <Sqlite as Backend>::RawValue<'_>) -> deserialize::Result<Self> {
        let blob = <Vec<u8> as FromSql<Binary, Sqlite>>::from_sql(bytes).map_err(|_| {
            Box::new(io::Error::new(
                io::ErrorKind::InvalidData,
                ERROR_EXPECTED_BLOB,
            ))
        })?;

        if blob.len() != 8 {
            return Err(Box::new(io::Error::new(
                io::ErrorKind::InvalidData,
                ERROR_INCORRECT_LENGTH,
            )));
        }

        // Convert to fixed-size array and then to u64.
        let bytes: [u8; 8] = blob.try_into().map_err(|_| {
            Box::new(io::Error::new(
                io::ErrorKind::InvalidData,
                ERROR_CONVERSION_FAILED,
            ))
        })?;

        Ok(U64Blob(u64::from_be_bytes(bytes)))
    }
}

#[cfg(test)]
mod tests {
    use diesel::{
        prelude::*,
        sql_types::{BigInt, Binary, Text},
        sqlite::SqliteConnection,
        QueryableByName,
    };

    use super::*;

    fn setup_test_db() -> SqliteConnection {
        let mut conn = SqliteConnection::establish(":memory:").unwrap();
        diesel::sql_query(
            "CREATE TABLE test_u64blobs (id INTEGER PRIMARY KEY, value BLOB NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();
        conn
    }

    #[derive(Debug, QueryableByName)]
    struct TestValueRow {
        #[diesel(sql_type = BigInt)]
        rowid: i64,

        #[diesel(sql_type = Binary)]
        value: U64Blob,

        #[diesel(sql_type = Text)]
        value_str: String,
    }

    #[test]
    fn test_roundtrip_numeric_and_string_values_with_rowid() {
        let mut conn = setup_test_db();
        let test_values = vec![0u64, 1u64, 42u64, u64::MAX, u64::MIN, 123456789];

        diesel::sql_query(
            "CREATE TABLE test_roundtrip_numeric_and_string_values_with_rowid (
            value BLOB NOT NULL,
            value_str TEXT NOT NULL)",
        )
        .execute(&mut conn)
        .unwrap();

        // Insert each value with its string representation.
        for &value in &test_values {
            let blob = U64Blob(value);
            let value_str = value.to_string();
            diesel::sql_query("INSERT INTO test_roundtrip_numeric_and_string_values_with_rowid (value, value_str) VALUES (?, ?)")
                .bind::<Binary, _>(blob)
                .bind::<Text, _>(value_str)
                .execute(&mut conn)
                .unwrap();
        }

        // Query the rows in descending order by rowid.
        let results: Vec<TestValueRow> = diesel::sql_query(
            "SELECT rowid, value, value_str FROM test_roundtrip_numeric_and_string_values_with_rowid ORDER BY rowid DESC",
        )
        .load(&mut conn)
        .unwrap();

        let total = test_values.len() as i64;
        let expected_values: Vec<u64> = test_values.iter().rev().copied().collect();
        let expected_rowids: Vec<i64> = (1..=total).rev().collect();

        for (i, row) in results.iter().enumerate() {
            assert_eq!(
                row.value.0, expected_values[i],
                "Numeric value mismatch at index {}",
                i
            );
            assert_eq!(
                row.value.0.to_string(),
                row.value_str,
                "String representation mismatch at index {}",
                i
            );
            assert_eq!(
                row.rowid, expected_rowids[i],
                "Rowid mismatch at index {}",
                i
            );
        }
    }
}
