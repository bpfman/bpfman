use std::{thread::sleep, time::Duration};

use assert_cmd::Command;
use log::debug;

use super::{integration_test, IntegrationTest};

#[integration_test]
fn test_load_unload() -> anyhow::Result<()> {
    let output = Command::cargo_bin("bpfctl")?
        .args([
            "load",
            "--iface",
            "eth0",
            "--priority",
            "50",
            "--from-image",
            "quay.io/astoycos/xdp_pass",
        ]).ok()?;
    let uuid = String::from_utf8(output.stdout)?;
    debug!("UUID is {:?}", uuid);
    sleep(Duration::from_secs(2));
    Command::cargo_bin("bpfctl")?
        .args(["unload", "--iface", "eth0", uuid.trim()])
        .assert()
        .success();
    debug!("Successfully deleted program: {}", uuid);
    Ok(())
}
