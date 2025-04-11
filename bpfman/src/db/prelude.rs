// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Commonly used database types and helpers.
//!
//! This module exposes domain-level types and column wrappers, but
//! **not** Diesel schema definitions like `bpf_programs::dsl::*`.
//! Schema elements are intentionally excluded to avoid polluting the
//! namespace with common column names like `id`, `name`, etc.
//! Instead, import schema elements explicitly where needed.

// Some of these imports are currently unused. We're migrating to
// SQLite, and only the `load` path is backed by the new DB layer so
// far. As more functionality is ported, the remaining types will be
// come into play.
#![allow(unused_imports)]
pub use super::{
    BpfLink, BpfMap, BpfProgram, BpfProgramMap, KernelU32, U8Blob, U16Blob, U32Blob, U64Blob,
    U128Blob, UxBlobError,
};
