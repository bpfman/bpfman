use std::{path::PathBuf, process::exit, thread::sleep, time::Duration};

use bpfd_api::util::directories::{RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP};
use log::{debug, error};
use rand::Rng;

use super::{integration_test, IntegrationTest};
use crate::tests::utils::*;

#[integration_test]
fn test_load_unload() {
    let guard = start_bpfd().unwrap();

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
    let uuid = add_xdp_pass(bpfd_iface, 75);
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, 50);
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, 100);
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(bpfd_iface, 25);
    uuids.push(uuid.unwrap());
    assert_eq!(uuids.len(), 4);

    // Verify the bppfs has entries
    assert!(PathBuf::from(RTDIR_FS_XDP)
        .read_dir()
        .unwrap()
        .next()
        .is_some());

    // Verify rule persistence between restarts
    drop(guard);
    let _guard = start_bpfd().unwrap();

    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(bpfctl_list.contains(id.trim()));
    }

    // Delete the installed programs
    debug!("Deleting bpfd programs");
    sleep(Duration::from_secs(2));
    for id in uuids.iter() {
        bpfd_del_program(id)
    }

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(!bpfctl_list.contains(id));
    }

    // Verify the bppfs is empty
    assert!(PathBuf::from(RTDIR_FS_XDP)
        .read_dir()
        .unwrap()
        .next()
        .is_none());
}

#[integration_test]
fn test_load_unload_tc() {
    let _guard = start_bpfd().unwrap();

    let bpfd_iface = read_iface_env();
    if !iface_exists(bpfd_iface) {
        error!(
            "Interface {} not found, specify a usable interface with the env BPFD_IFACE",
            bpfd_iface
        );
        exit(0)
    }

    debug!("Installing tc_pass programs");
    let mut uuids = vec![];
    let mut rng = rand::thread_rng();
    // Install a few xdp programs
    for _ in 0..10 {
        let priority = rng.gen_range(1..255);
        let uuid = add_tc_pass(bpfd_iface, priority);
        uuids.push(uuid.unwrap());
    }
    assert_eq!(uuids.len(), 10);

    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(bpfctl_list.contains(id.trim()));
    }

    // Verify TC filter is using correct priority
    let output = tc_filter_list(bpfd_iface).unwrap();
    assert!(output.contains("pref 49"));

    // Verify the bppfs has entries
    assert!(PathBuf::from(RTDIR_FS_TC_INGRESS)
        .read_dir()
        .unwrap()
        .next()
        .is_some());

    // Delete the installed programs
    debug!("Deleting bpfd programs");
    sleep(Duration::from_secs(2));
    for id in uuids.iter() {
        bpfd_del_program(id)
    }

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(!bpfctl_list.contains(id.trim()));
    }

    // Verify the bppfs is empty
    assert!(PathBuf::from(RTDIR_FS_TC_INGRESS)
        .read_dir()
        .unwrap()
        .next()
        .is_none());

    let output = tc_filter_list(bpfd_iface).unwrap();
    assert!(output.trim().is_empty());
}

#[integration_test]
fn test_load_unload_tracepoint() {
    let _guard = start_bpfd().unwrap();

    debug!("Installing tracepoint programs");
    let uuid = add_tracepoint().unwrap();
    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    assert!(bpfctl_list.contains(uuid.trim()));

    // Delete the installed program
    debug!("Deleting bpfd programs");
    sleep(Duration::from_secs(2));
    bpfd_del_program(&uuid);

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list().unwrap();
    assert!(!bpfctl_list.contains(&uuid));
}
