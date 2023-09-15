use std::process::Command;

use assert_cmd::prelude::*;
use bpfd_api::util::directories::{
    BYTECODE_IMAGE_CONTENT_STORE, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP,
};
use log::debug;
use rand::Rng;

use super::{integration_test, IntegrationTest};
use crate::tests::utils::*;

#[integration_test]
fn test_bpfctl_helptext() {
    let args = vec!["list", "-help"];

    assert!(!Command::cargo_bin("bpfctl")
        .unwrap()
        .args(args)
        .ok()
        .expect("bpfctl list --help failed")
        .stdout
        .is_empty());
}

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

    let mut loaded_ids = vec![];
    let mut rng = rand::thread_rng();

    // Install a few xdp programs
    for lt in LOAD_TYPES {
        for _ in 0..5 {
            let priority = rng.gen_range(1..255);
            let (prog_id, _) = add_xdp(
                DEFAULT_BPFD_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                XDP_PASS_IMAGE_LOC,
                XDP_PASS_FILE_LOC,
                None,
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }
    assert_eq!(loaded_ids.len(), 10);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(loaded_ids);

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

    let mut loaded_ids = vec![];
    let mut rng = rand::thread_rng();

    // Install a few tc programs
    for lt in LOAD_TYPES {
        for _ in 0..5 {
            let priority = rng.gen_range(1..255);
            let (prog_id, _) = add_tc(
                "ingress",
                DEFAULT_BPFD_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                TC_PASS_IMAGE_LOC,
                TC_PASS_FILE_LOC,
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }
    assert_eq!(loaded_ids.len(), 10);

    // Verify TC filter is using correct priority
    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.contains("pref 50"));
    assert!(output.contains("handle 0x2"));

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    let output = tc_filter_list(DEFAULT_BPFD_IFACE).unwrap();
    assert!(output.trim().is_empty());
}

#[integration_test]
fn test_load_unload_tracepoint() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing tracepoint programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let (prog_id, _) = add_tracepoint(
            Some(globals.clone()),
            lt,
            TRACEPOINT_IMAGE_LOC,
            TRACEPOINT_FILE_LOC,
        );
        loaded_ids.push(prog_id.unwrap());
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_uprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing uprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_uprobe(
            Some(globals.clone()),
            lt,
            UPROBE_IMAGE_LOC,
            URETPROBE_FILE_LOC,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_uretprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing uretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_uretprobe(
            Some(globals.clone()),
            lt,
            URETPROBE_IMAGE_LOC,
            URETPROBE_FILE_LOC,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_kprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing kprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id =
            add_kprobe(Some(globals.clone()), lt, KPROBE_IMAGE_LOC, KPROBE_FILE_LOC).unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_kretprobe() {
    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Installing kretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids: Vec<String> = vec![];

    // Load some kretprobes
    for lt in LOAD_TYPES {
        let prog_id = add_kretprobe(
            Some(globals.clone()),
            lt,
            KRETPROBE_IMAGE_LOC,
            KRETPROBE_FILE_LOC,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_pull_bytecode() {
    if std::path::PathBuf::from(BYTECODE_IMAGE_CONTENT_STORE).exists() {
        std::fs::remove_dir_all(BYTECODE_IMAGE_CONTENT_STORE).unwrap();
    }

    let _bpfd_guard = start_bpfd().unwrap();

    debug!("Pull bytecode image");

    let _result = bpfd_pull_bytecode().unwrap();

    let path = get_image_path();
    assert!(path.exists());
}

#[integration_test]
fn test_list_with_metadata() {
    let _namespace_guard = create_namespace().unwrap();
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

    let mut loaded_ids = vec![];
    let mut rng = rand::thread_rng();

    // Install a few xdp programs
    for lt in LOAD_TYPES {
        for _ in 0..2 {
            let priority = rng.gen_range(1..255);
            let (prog_id, _) = add_xdp(
                DEFAULT_BPFD_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                XDP_PASS_IMAGE_LOC,
                XDP_PASS_FILE_LOC,
                None,
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }

    let key = "uuid=ITS_BPF_NOT_EBPF";
    let priority = rng.gen_range(1..255);
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFD_IFACE,
        priority,
        Some(globals.clone()),
        Some(proceed_on.clone()),
        &LoadType::Image,
        XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        Some(vec![key]),
    );
    let id = prog_id.unwrap();

    debug!("Listing programs with metadata {key}");
    // ensure listing with metadata works
    let list_output = bpfd_list(Some(vec![key])).unwrap();

    assert!(list_output.contains(&id));

    loaded_ids.push(id);

    assert_eq!(loaded_ids.len(), 5);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}
