use std::{process::Command, thread::sleep, time::Duration};

use anyhow::Result;
use assert_cmd::prelude::*;
use log::debug;
use predicates::str::is_empty;

const DEFAULT_BPFD_IFACE: &str = "eth0";
const XDP_PASS_IMAGE: &str = "quay.io/bpfd/bytecode:xdp_pass";
const TC_PASS_IMAGE: &str = "quay.io/bpfd/bytecode:tc_pass";
const TRACEPOINT_IMAGE: &str = "quay.io/bpfd/bytecode:tracepoint";

/// Exit on panic as well as the passing of a test
pub struct ChildGuard {
    name: &'static str,
    child: std::process::Child,
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        if let Err(e) = self.child.kill() {
            println!("Could not kill {}: {e}", self.name);
        }
        if let Err(e) = self.child.wait() {
            println!("Could not wait for {}: {e}", self.name);
        }
    }
}

/// Spawn a bpfd process
pub fn start_bpfd() -> Result<ChildGuard> {
    debug!("Starting bpfd");
    let bpfd_process = Command::cargo_bin("bpfd")?.spawn().map(|c| ChildGuard {
        name: "bpfd",
        child: c,
    })?;
    sleep(Duration::from_secs(2));
    debug!("Successfully Started bpfd");
    Ok(bpfd_process)
}

/// Check for a specified bpfd interface environmental, otherwise set a default
pub fn read_iface_env() -> &'static str {
    option_env!("BPFD_IFACE").unwrap_or(DEFAULT_BPFD_IFACE)
}

/// Install an xdp_pass program with bpfctl
pub fn add_xdp_pass(iface: &str, priority: u32) -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?
        .args([
            "load",
            "--from-image",
            "--path",
            XDP_PASS_IMAGE,
            "xdp",
            "--iface",
            iface,
            "--priority",
            priority.to_string().as_str(),
        ])
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let uuid = stdout.trim();
    assert!(!uuid.is_empty());
    debug!("Successfully added xdp_pass program: {:?}", uuid);

    Ok(uuid.to_string())
}

/// Install an xdp_pass program with bpfctl
pub fn add_tc_pass(iface: &str, priority: u32) -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?
        .args([
            "load",
            "--from-image",
            "--path",
            TC_PASS_IMAGE,
            "tc",
            "--direction",
            "ingress",
            "--iface",
            iface,
            "--priority",
            priority.to_string().as_str(),
        ])
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let uuid = stdout.trim();
    assert!(!uuid.is_empty());
    debug!("Successfully added tc_pass program: {:?}", uuid);
    Ok(uuid.to_string())
}

/// Install an tracepoint program with bpfctl
pub fn add_tracepoint() -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?
        .args([
            "load",
            "--from-image",
            "--path",
            TRACEPOINT_IMAGE,
            "tracepoint",
            "--tracepoint",
            "syscalls/sys_enter_openat",
        ])
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let uuid = stdout.trim();
    assert!(!uuid.is_empty());
    debug!("Successfully added tracepoint program: {:?}", uuid);
    Ok(uuid.to_string())
}

/// Delete a bpfd program using bpfctl
pub fn bpfd_del_program(uuid: &str) {
    Command::cargo_bin("bpfctl")
        .unwrap()
        .args(["unload", uuid.trim()])
        .assert()
        .success()
        .stdout(is_empty());

    debug!("Successfully deleted program: \"{}\"", uuid.trim());
}

/// Retrieve the output of bpfctl list
pub fn bpfd_list() -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?.args(["list"]).ok();
    let stdout = String::from_utf8(output.unwrap().stdout);

    Ok(stdout.unwrap())
}

/// Retrieve the output of bpfctl list
pub fn tc_filter_list(iface: &str) -> Result<String> {
    let output = Command::new("tc")
        .args(["filter", "show", "dev", iface, "ingress"])
        .output()
        .expect("tc filter show dev lo ingress");
    let stdout = String::from_utf8(output.unwrap().stdout);
    Ok(stdout.unwrap())
}

/// Verify the specified interface exists
/// TODO: make OS agnostic (network-interface crate https://lib.rs/crates/network-interface?)
pub fn iface_exists(bpfd_iface: &str) -> bool {
    let output = Command::new("ip")
        .args(["link", "show"])
        .output()
        .expect("ip link show");
    let link_out = String::from_utf8(output.stdout).unwrap();

    if link_out.contains(bpfd_iface) {
        return true;
    }

    false
}
