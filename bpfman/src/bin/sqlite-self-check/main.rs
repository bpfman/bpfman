// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::Error;

// XXX Can be removed in the future.
fn main() -> anyhow::Result<(), Error> {
    // Temporary check to confirm whether SQLite is linked statically
    // or dynamically. Verify linkage using: `ldd
    // ./target/debug/bpfman`
    //
    // Since the `bundled` feature is enabled in `Cargo.toml` for
    // `libsqlite3-sys`, SQLite should be statically linked, removing
    // the dependency on system-installed SQLite.
    //
    // Additionally, this test ensures that the database schema is
    // fully embedded within the binary. You can move the
    // `migrations/` directory aside and still successfully create a
    // database, confirming that the schema does not rely on external
    // migration files.
    let _ = bpfman::establish_sqlite_connection(":memory:")?;
    Ok(())
}
