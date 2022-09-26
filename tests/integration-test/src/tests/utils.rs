use std::{process::Command, thread::sleep, time::Duration};

use anyhow::Result;
use assert_cmd::prelude::*;
use log::debug;
use predicates::str::is_empty;

const DEFAULT_BPFD_IFACE: &str = "eth0";
const XDP_PASS_IMAGE: &str = "quay.io/bpfd/bytecode:xdp_pass";

/// Exit on panic as well as the passing of a test
pub struct ChildGuard {
    name: &'static str,
    child: std::process::Child,
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        if let Err(e) = self.child.kill() {
            println!("Could not kill {}: {}", self.name, e);
        }
        if let Err(e) = self.child.wait() {
            println!("Could not wait for {}: {}", self.name, e);
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
pub fn add_xdp_pass(iface: &str, priority: &str) -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?
        .args([
            "load",
            "--iface",
            iface,
            "--priority",
            priority,
            "--from-image",
            XDP_PASS_IMAGE,
        ])
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout);
    let uuid = stdout.as_ref();
    assert!(!uuid.unwrap().is_empty());
    debug!(
        "Successfully added xdp_pass program: {:?}",
        uuid.unwrap().trim()
    );

    Ok(stdout.unwrap())
}

/// Delete a bpfd program using bpfctl
pub fn bpfd_del_program(bpfd_iface: &str, uuid: &str) {
    Command::cargo_bin("bpfctl")
        .unwrap()
        .args(["unload", "--iface", bpfd_iface, uuid.trim()])
        .assert()
        .success()
        .stdout(is_empty());

    debug!("Successfully deleted program: \"{}\"", uuid.trim());
}

/// Retrieve the output of bpfctl list
pub fn bpfd_list(bpfd_iface: &str) -> Result<String> {
    let output = Command::cargo_bin("bpfctl")?
        .args(["list", "--iface", bpfd_iface])
        .ok();
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
