// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Diesel model definitions for bpfman.
//!
//! This module defines domain-level types that correspond to rows in
//! the database. These types implement Diesel traits like
//! `Queryable`, `Insertable`, and `Identifiable`.
//!
//! Each entity (program, map, link, etc.) has its own submodule for
//! clarity and separation of concerns.
//!
//! # Design Notes: No `Default` for Model Types
//!
//! The model types in this module (`BpfProgram`, `BpfMap`, etc.) do
//! **not** implement `Default` in production code. This is
//! intentional:
//!
//! - These types represent persisted data with required fields.
//!
//! - Constructing a zeroed or placeholder version risks violating
//!   database constraints or semantic invariants.
//!
//! - Diesel’s insert/update operations rely on fully initialised
//!   data.
//!
//! We *do* implement `Default` under `#[cfg(test)]` to support unit
//! tests and reduce boilerplate, but this is limited to internal use.
//!
//! Consumers should construct model instances explicitly.
//!
//! # Diesel Trait Derivations
//!
//! These traits are commonly derived on Diesel model structs to enable
//! interaction with the database:
//!
//! - [`diesel::Queryable`]: Loads rows from the database into Rust structs.
//! - [`diesel::Insertable`]: Enables inserting structs as new rows.
//! - [`diesel::Identifiable`]: Associates a struct with a primary key.
//! - [`diesel::AsChangeset`]: Updates rows by mapping struct fields to columns.
//! - [`diesel::associations::HasTable`]: Required by various table operations.
//! - [`diesel::Associations`]: Enables modelling relationships (`belongs_to`, etc.).
//! - [`diesel::Selectable`]: Enables typed column projections.
//! - [`diesel::QueryableByName`]: Used with raw SQL queries via `sql_query(...)`.
//!
//! These traits are usually brought into scope with `use
//! diesel::prelude::*`.

mod bpf_link;
mod bpf_map;
mod bpf_program;
mod bpf_program_map;

pub use bpf_link::*;
pub use bpf_map::*;
pub use bpf_program::*;
pub use bpf_program_map::*;
