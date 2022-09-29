use std::{process::exit, thread::sleep, time::Duration};

use log::{debug, error};

use super::{integration_test, IntegrationTest};
use crate::tests::utils::*;

#[integration_test]
fn test_load_unload() -> anyhow::Result<()> {
    let _guard = start_bpfd()?;

    let bpfd_iface = read_iface_env();
    if !iface_exists(bpfd_iface) {
        error!(
            "Interface {} not found, specify a usable interface with the env BPFD_IFACE",
            bpfd_iface
        );
        exit(0)
    }

    debug!("Installing xdp_pass programs");
    let mut uuids = vec![];
    // Install a few xdp programs
    let uuid = add_xdp_pass(bpfd_iface, "75");
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, "50");
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, "100");
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, "25");
    uuids.push(uuid.unwrap());
    assert_eq!(uuids.len(), 4);

    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list(bpfd_iface);
    let bpfctl_list = bpfctl_list.as_ref();
    for id in uuids.iter() {
        let prog_list = bpfctl_list.unwrap();
        assert!(prog_list.contains(id.trim()));
    }

    // Delete the installed programs
    debug!("Deleting bpfd programs");
    sleep(Duration::from_secs(2));
    for id in uuids.iter() {
        bpfd_del_program(bpfd_iface, id)
    }

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list(bpfd_iface);
    let bpfctl_list = bpfctl_list.as_ref();
    for id in uuids.iter() {
        let prog_list = bpfctl_list.unwrap();
        assert!(!prog_list.contains(id));
    }

    Ok(())
}
