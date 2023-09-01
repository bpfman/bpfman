use bpfd_api::util::directories::{
    BYTECODE_IMAGE_CONTENT_STORE, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP,
};
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
    for lt in LOAD_TYPES {
        let uuid = add_xdp_pass(
            DEFAULT_BPFD_IFACE,
            75,
            Some(globals.clone()),
            Some(proceed_on.clone()),
            lt,
        );
        uuids.push(uuid.unwrap());
        let uuid = add_xdp_pass(
            DEFAULT_BPFD_IFACE,
            50,
            Some(globals.clone()),
            Some(proceed_on.clone()),
            lt,
        );
        uuids.push(uuid.unwrap());
        let uuid = add_xdp_pass(
            DEFAULT_BPFD_IFACE,
            100,
            Some(globals.clone()),
            Some(proceed_on.clone()),
            lt,
        );
        uuids.push(uuid.unwrap());
        let uuid = add_xdp_pass(DEFAULT_BPFD_IFACE, 25, None, None, lt);
        uuids.push(uuid.unwrap());
    }
    assert_eq!(uuids.len(), 8);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(uuids);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
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
    for lt in LOAD_TYPES {
        for _ in 0..5 {
            let priority = rng.gen_range(1..255);
            let uuid = add_tc_pass(
                "ingress",
                DEFAULT_BPFD_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
            );
            uuids.push(uuid.unwrap());
        }
    }
    assert_eq!(uuids.len(), 10);

    // Verify TC filter is using correct priority
    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.contains("pref 50"));
    assert!(output.contains("handle 0x2"));

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    verify_and_delete_programs(uuids);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.trim().is_empty());
}

#[integration_test]
fn test_load_unload_tracepoint() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing tracepoint programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let mut uuids = vec![];

    for lt in LOAD_TYPES {
        let uuid = add_tracepoint(Some(globals.clone()), lt).unwrap();
        uuids.push(uuid);
    }

    verify_and_delete_programs(uuids);
}

#[integration_test]
fn test_load_unload_uprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing uprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut uuids = vec![];

    for lt in LOAD_TYPES {
        let uuid = add_uprobe(Some(globals.clone()), lt).unwrap();
        uuids.push(uuid);
    }

    verify_and_delete_programs(uuids);
}

#[integration_test]
fn test_load_unload_uretprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing uretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut uuids = vec![];

    for lt in LOAD_TYPES {
        let uuid = add_uretprobe(Some(globals.clone()), lt).unwrap();
        uuids.push(uuid);
    }

    verify_and_delete_programs(uuids);
}

#[integration_test]
fn test_load_unload_kprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing kprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut uuids = vec![];

    for lt in LOAD_TYPES {
        let uuid = add_kprobe(Some(globals.clone()), lt).unwrap();
        uuids.push(uuid);
    }

    verify_and_delete_programs(uuids);
}

#[integration_test]
fn test_load_unload_kretprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing kretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut uuids: Vec<String> = vec![];

    // Load some kretprobes
    for lt in LOAD_TYPES {
        let uuid = add_kretprobe(Some(globals.clone()), lt).unwrap();
        uuids.push(uuid);
    }

    verify_and_delete_programs(uuids);
}

#[integration_test]
fn test_pull_bytecode() {
    std::fs::remove_dir_all(BYTECODE_IMAGE_CONTENT_STORE).unwrap();

    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Pull bytecode image");

    let _result = bpfd_pull_bytecode().unwrap();

    let path = get_image_path();
    assert!(path.exists());
}
