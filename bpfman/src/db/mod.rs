// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Top-level database module.
//!
//! This module re-exports types and helpers used throughout the
//! crate, flattening the module structure so consumers can simply
//! use crate::db::KernelU32;
//!
//! The `types` module and its internal submodules are kept private to
//! encourage consistent usage and avoid exposing internal structure.
//!
//! If a type appears in `crate::db`, it's part of the crate's
//! intended public interface.

use diesel::{Connection, prelude::*, sqlite::SqliteConnection};
use diesel_migrations::{EmbeddedMigrations, MigrationHarness, embed_migrations};

use crate::BpfmanError;

pub mod models;
pub mod prelude;
pub mod schema;
mod types;
pub use models::*;
pub use schema::{bpf_links, bpf_maps, bpf_program_maps, bpf_programs};
// Re-export database column types and wrappers for public use. These
// types are defined in the private `types` module and exposed here to
// flatten the db module API.
//
// See also: `crate::db::prelude` for glob imports.
pub use types::{KernelU32, U8Blob, U16Blob, U32Blob, U64Blob, U128Blob, UxBlobError};

const MIGRATIONS: EmbeddedMigrations = embed_migrations!("migrations");

/// Internal note: This function is `pub` only to support integration
/// tests and CLI binaries. It is not part of the stable public API
/// and should be considered an implementation detail.
///
/// Ideally, this would remain `pub(crate)`, but Rust’s visibility
/// rules require it to be `pub` for use outside the crate (e.g., in
/// `src/bin/` or integration tests).
///
/// Why this is considered undesirable:
///
/// - Exposes low-level persistence machinery that is intended for
///   internal use only.
/// - Requires external consumers (e.g. tests, binaries) to be aware of
///   Diesel internals, PRAGMA settings, and connection semantics.
/// - Inconsistent with the higher-level abstractions provided elsewhere
///   (such as `load_ebpf_programs`, which encapsulates both kernel and
///   DB logic).
/// - Becomes an API liability if the backing storage engine (e.g.
///   Diesel) is ever swapped out or wrapped differently.
///
/// ---
///
/// Establish a new SQLite database connection and run pending
/// migrations.
///
/// This function connects to the SQLite database at the provided URL,
/// applies standard PRAGMA settings, and runs any unapplied schema
/// migrations embedded in the binary.
///
/// The following PRAGMAs are set to configure the database's
/// behaviour:
///
/// - `journal_mode = WAL`: Enables Write-Ahead Logging for improved
///   concurrency and performance in multi-writer scenarios.
/// - `busy_timeout = 5000`: Sets a 5-second timeout when the database
///   is locked, to avoid immediate failure under contention.
/// - `foreign_keys = ON`: Enforces foreign key constraints at the
///   database level.
///
/// After applying PRAGMAs, all pending migrations are executed using
/// `diesel_migrations::MigrationHarness::run_pending_migrations`.
///
/// # Arguments
///
/// * `database_url` – Path or URI to the SQLite database file.
///
/// # Errors
///
/// Returns a [`BpfmanError`] if:
///
/// - The connection cannot be established.
/// - Any of the PRAGMA statements fail to execute.
/// - One or more schema migrations fail to apply.
pub fn establish_database_connection(database_url: &str) -> Result<SqliteConnection, BpfmanError> {
    let mut conn = SqliteConnection::establish(database_url).map_err(|e| {
        BpfmanError::SqliteConnectionError {
            database_url: database_url.to_string(),
            source: e,
        }
    })?;

    diesel::sql_query("PRAGMA journal_mode = WAL")
        .execute(&mut conn)
        .map_err(|e| BpfmanError::SqliteQueryError {
            database_url: database_url.to_string(),
            context: "setting WAL journal mode".into(),
            source: e,
        })?;

    diesel::sql_query("PRAGMA busy_timeout = 5000")
        .execute(&mut conn)
        .map_err(|e| BpfmanError::SqliteQueryError {
            database_url: database_url.to_string(),
            context: "setting busy timeout".into(),
            source: e,
        })?;

    diesel::sql_query("PRAGMA foreign_keys = ON")
        .execute(&mut conn)
        .map_err(|e| BpfmanError::SqliteQueryError {
            database_url: database_url.to_string(),
            context: "enabling foreign key support".into(),
            source: e,
        })?;

    conn.run_pending_migrations(MIGRATIONS)
        .map_err(|e| BpfmanError::SqliteMigrationError {
            database_url: database_url.to_string(),
            source: e,
        })?;

    Ok(conn)
}
