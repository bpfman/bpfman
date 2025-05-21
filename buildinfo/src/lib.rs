// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Shared build-time version generation for bpfman binaries.
//!
//! This library provides a minimal public API for build.rs scripts
//! across the bpfman project to generate consistent version
//! information.

use std::{env, fmt::Write, process::Command};

use chrono::Utc;

/// Generates version information and sets up rerun conditions.
///
/// This is the main entry point and only public function of this
/// crate. It performs two actions:
///
/// 1. Sets the BPFMAN_BUILD_INFO environment variable for the build
/// 2. Configures when Cargo should re-run the build script
///
/// The BPFMAN_BUILD_INFO format is: `<project-version> (<git-hash>
/// <build-timestamp>) <rustc-version>`.
pub fn generate_version_info() {
    println!("cargo:rerun-if-changed=src");
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed=Cargo.toml");
    println!("cargo:rerun-if-env-changed=BPFMAN_BUILD_TIMESTAMP");
    println!("cargo:rustc-env=BPFMAN_BUILD_INFO={}", build_info_string());
}

/// Returns a human-readable Git version string for embedding in build
/// metadata.
///
/// This function attempts to describe the current Git commit using:
///
/// ```sh
/// git describe --tags --always --dirty
/// ```
///
/// The output has the form:
/// - `v0.5.6` (exact tag)
/// - `v0.5.6-129-g77959d44` (129 commits after `v0.5.6`)
/// - `v0.5.6-129-g77959d44-dirty` (same, with uncommitted changes)
/// - `g77959d44` (no tag present, fallback to short commit hash)
///
/// If `git describe` fails (e.g., the repo has no tags), this
/// function falls back to `git rev-parse --short=10 HEAD` combined
/// with a manual check for uncommitted changes via `git status
/// --porcelain`.
///
/// This fallback ensures version information is still available in
/// untagged repositories.
///
/// Both code paths require a functioning Git repository (i.e., access
/// to `.git`). If the build is performed from a source tarball or
/// outside Git, this function returns `None`, and no Git version
/// information is embedded.
///
/// Returns:
/// - `Some(version_string)` if Git metadata is available
/// - `None` if Git is not available or the repository is missing
fn get_git_version() -> Option<String> {
    // Try `git describe` first.
    let describe = Command::new("git")
        .args(["describe", "--tags", "--always", "--dirty"])
        .output()
        .ok()
        .filter(|output| output.status.success())
        .and_then(|output| {
            String::from_utf8(output.stdout)
                .ok()
                .map(|s| s.trim().to_string())
        });

    if describe.is_some() {
        return describe;
    }

    // Fallback: short hash + dirty suffix.
    let commit_hash = Command::new("git")
        .args(["rev-parse", "--short=10", "HEAD"])
        .output()
        .ok()
        .filter(|output| output.status.success())
        .and_then(|output| {
            String::from_utf8(output.stdout)
                .ok()
                .map(|s| s.trim().to_string())
        });

    let is_dirty = Command::new("git")
        .args(["status", "--porcelain"])
        .output()
        .map(|out| !out.stdout.is_empty())
        .unwrap_or(false);

    let dirty_suffix = if is_dirty { "-dirty" } else { "" };
    commit_hash.map(|hash| format!("{hash}{dirty_suffix}"))
}

/// Gets the build timestamp from env or uses now.
fn get_build_timestamp() -> String {
    env::var("BPFMAN_BUILD_TIMESTAMP")
        .ok()
        .filter(|s| !s.trim().is_empty())
        .unwrap_or_else(|| Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string())
}

/// Gets the Rust toolchain version.
fn get_rustc_version() -> Option<String> {
    Command::new("rustc")
        .arg("--version")
        .output()
        .ok()
        .and_then(|output| String::from_utf8(output.stdout).ok())
        .map(|s| s.trim().to_string())
}

/// Gets the Git repository origin URL. Returns `None` if Git is
/// unavailable or not a repo.
fn get_git_origin() -> Option<String> {
    use std::process::Command;
    Command::new("git")
        .args(["config", "--get", "remote.origin.url"])
        .output()
        .ok()
        .and_then(|output| String::from_utf8(output.stdout).ok())
        .map(|s| s.trim().to_string())
}

/// Computes the authoritative build info string.
fn build_info_string() -> String {
    let build_date = get_build_timestamp();
    let git_version = get_git_version();
    let git_origin = get_git_origin();
    let rustc_version = get_rustc_version();
    let version = env!("CARGO_PKG_VERSION");

    let mut out = String::with_capacity(120);

    write!(&mut out, "{version} (").unwrap();

    if let Some(git) = git_version {
        write!(&mut out, "{git} ").unwrap();
    }

    if let Some(origin) = git_origin {
        write!(&mut out, "{origin} ").unwrap();
    }

    write!(&mut out, "{build_date})").unwrap();

    if let Some(rustc) = rustc_version {
        write!(&mut out, " {rustc}").unwrap();
    }

    out
}
