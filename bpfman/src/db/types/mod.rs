// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Column-level database types and wrappers.
//!
//! This internal module defines helper types used in Diesel models
//! for representing non-standard Rust values in SQLite:
//!
//! - [`KernelU32`]: stores `u32` values in SQLite `BIGINT` columns
//! - [`U64Blob`], [`U128Blob`], etc.: store fixed-size integers in
//!   BLOB columns with strict encoding
//!
//! These wrappers provide:
//!
//! - Type safety for numeric IDs and identifiers
//! - Fixed-size binary encoding (for BLOB columns)
//! - Integration with Diesel (`ToSql`, `FromSql`, `AsExpression`, etc.)
//! - Deserialisation-time size checks and conversions
//!
//! These are *not* domain types like [`BpfProgram`] or [`BpfMap`].
//! For those, see the per-entity modules (`bpf_program.rs`, etc.).
//!
//! All types here are re-exported at the `crate::db` level, so users
//! can write `use crate::db::KernelU32` without referring to
//! implementation details.

mod ku32;
mod uintblob;

pub use self::{
    ku32::KernelU32,
    uintblob::{U8Blob, U16Blob, U32Blob, U64Blob, U128Blob, UxBlobError},
};
