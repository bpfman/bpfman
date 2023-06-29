use std::path::PathBuf;

use bpfd_api::util::directories::{RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP};
use log::debug;
use rand::Rng;

use super::{integration_test, IntegrationTest};
use crate::tests::utils::*;

#[integration_test]
fn test_load_unload_xdp() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    debug!("Installing xdp_pass programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let proceed_on = vec![
        "aborted",
        "drop",
        "pass",
        "tx",
        "redirect",
        "dispatcher_return",
    ];

    let mut uuids = vec![];
    // Install a few xdp programs
    let uuid = add_xdp_pass(
        DEFAULT_BPFD_IFACE,
        75,
        Some(globals.clone()),
        Some(proceed_on.clone()),
    );
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(
        DEFAULT_BPFD_IFACE,
        50,
        Some(globals.clone()),
        Some(proceed_on.clone()),
    );
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(
        DEFAULT_BPFD_IFACE,
        100,
        Some(globals.clone()),
        Some(proceed_on.clone()),
    );
    uuids.push(uuid.unwrap());
    let uuid = add_xdp_pass(DEFAULT_BPFD_IFACE, 25, None, None);
    uuids.push(uuid.unwrap());
    assert_eq!(uuids.len(), 4);

    // Verify the bppfs has entries
    assert!(PathBuf::from(RTDIR_FS_XDP)
        .read_dir()
        .unwrap()
        .next()
        .is_some());

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(bpfctl_list.contains(id.trim()));
    }

    // Delete the installed programs
    debug!("Deleting bpfd programs");
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
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let _bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    debug!("Installing ingress tc programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let proceed_on = vec![
        "unspec",
        "ok",
        "reclassify",
        "shot",
        "pipe",
        "stolen",
        "queued",
        "repeat",
        "redirect",
        "trap",
        "dispatcher_return",
    ];

    let mut uuids = vec![];
    let mut rng = rand::thread_rng();
    // Install a few tc programs
    for _ in 0..10 {
        let priority = rng.gen_range(1..255);
        let uuid = add_tc_pass(
            "ingress",
            DEFAULT_BPFD_IFACE,
            priority,
            Some(globals.clone()),
            Some(proceed_on.clone()),
        );
        uuids.push(uuid.unwrap());
    }
    assert_eq!(uuids.len(), 10);

    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    for id in uuids.iter() {
        assert!(bpfctl_list.contains(id.trim()));
    }

    // Verify TC filter is using correct priority
    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.contains("pref 50"));
    assert!(output.contains("handle 0x2"));

    // Verify the bppfs has entries
    assert!(PathBuf::from(RTDIR_FS_TC_INGRESS)
        .read_dir()
        .unwrap()
        .next()
        .is_some());

    // Delete the installed programs
    debug!("Deleting bpfd programs");
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

    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.trim().is_empty());
}

#[integration_test]
fn test_load_unload_tracepoint() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing tracepoint programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let uuid = add_tracepoint(Some(globals)).unwrap();
    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    assert!(bpfctl_list.contains(uuid.trim()));

    // Delete the installed program
    debug!("Deleting bpfd programs");
    bpfd_del_program(&uuid);

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list().unwrap();
    assert!(!bpfctl_list.contains(&uuid));
}

#[integration_test]
fn test_load_unload_uprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing uprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let uuid = add_uprobe(Some(globals)).unwrap();
    // Verify bpfctl list contains the uuids of each program
    let bpfctl_list = bpfd_list().unwrap();
    assert!(bpfctl_list.contains(uuid.trim()));

    // Delete the installed program
    debug!("Deleting bpfd programs");
    bpfd_del_program(&uuid);

    // Verify bpfctl list does not contain the uuids of the deleted programs
    // and that there are no panics if bpfctl does not contain any programs.
    let bpfctl_list = bpfd_list().unwrap();
    assert!(!bpfctl_list.contains(&uuid));
}
