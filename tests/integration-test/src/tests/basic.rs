use std::process::Command;

use assert_cmd::prelude::*;
use log::debug;
use rand::Rng;

use super::{integration_test, IntegrationTest, RTDIR_FS_MAPS, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP};
use crate::tests::utils::*;

#[integration_test]
fn test_bpfmanhelptext() {
    let args = vec!["list", "-help"];

    assert!(!Command::cargo_bin("bpfman")
        .unwrap()
        .args(args)
        .ok()
        .expect("bpfman list --help failed")
        .stdout
        .is_empty());
}

#[integration_test]
fn test_load_unload_xdp() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    //let bpfman_guard = start_bpfman().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

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
                DEFAULT_BPFMAN_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                &XDP_PASS_IMAGE_LOC,
                XDP_PASS_FILE_LOC,
                Some(XDP_PASS_NAME),
                None, // metadata
                None, // map_owner_id
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }
    assert_eq!(loaded_ids.len(), 10);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    // Verify rule persistence between restarts
    // drop(bpfman_guard);
    // let _bpfman_guard = start_bpfman().unwrap();

    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[integration_test]
fn test_map_sharing_load_unload_xdp() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    //let bpfman_guard = start_bpfman().unwrap();
    let load_type = LoadType::Image;

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    // Load first program, which will own the map.
    debug!("Installing xdp_counter map owner program 1");
    let (map_owner_id, stdout_1) = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        50,
        None, // globals
        None, // proceed_on
        &load_type,
        &XDP_COUNTER_IMAGE_LOC,
        "", // file_path
        None,
        None, // metadata
        None, // map_owner_id
    );
    let binding_1 = stdout_1.unwrap();

    // Verify "Map Used By:" field is set to only the just loaded program.
    let map_used_by_1 = bpfman_output_xdp_map_used_by(&binding_1);
    assert!(map_used_by_1.len() == 1);
    assert!(map_used_by_1[0] == *(map_owner_id.as_ref().unwrap()));

    let map_ids_1 = bpfman_output_map_ids(&binding_1);

    // Verify the "Map Owner Id:" is None.
    let map_owner_id_1 = bpfman_output_map_owner_id(&binding_1);
    assert!(map_owner_id_1 == "None");

    // Verify the "Map Pin Path:" is set properly.
    let map_pin_path = RTDIR_FS_MAPS.to_string() + "/" + map_owner_id.as_ref().unwrap();
    let map_pin_path_1 = bpfman_output_map_pin_path(&binding_1);
    assert!(map_pin_path_1 == map_pin_path);

    // Load second program, which will share the map with the first program.
    debug!("Installing xdp_counter map sharing program 2");
    let map_owner_id_u32 = match map_owner_id.as_ref().unwrap().parse() {
        Ok(v) => Some(v),
        Err(_) => None,
    };
    let (shared_owner_id, stdout_2) = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        50,   // priority
        None, // globals
        None, // proceed_on
        &load_type,
        &XDP_COUNTER_IMAGE_LOC,
        "", // file_path
        None,
        None, // metadata
        map_owner_id_u32,
    );
    let binding_2 = stdout_2.unwrap();

    // Verify the "Map Used By:" field is set to both loaded program.
    // Order of programs is not guarenteed.
    let map_used_by_2 = bpfman_output_xdp_map_used_by(&binding_2);
    assert!(map_used_by_2.len() == 2);
    assert!(
        map_used_by_2[0] == *(map_owner_id.as_ref().unwrap())
            || map_used_by_2[1] == *(map_owner_id.as_ref().unwrap())
    );
    assert!(
        map_used_by_2[0] == *(shared_owner_id.as_ref().unwrap())
            || map_used_by_2[1] == *(shared_owner_id.as_ref().unwrap())
    );

    let map_ids_2 = bpfman_output_map_ids(&binding_2);
    // Ensure the map IDs for both programs are the same
    assert_eq!(map_ids_1, map_ids_2);

    // Verify the "Map Owner Id:" is set to map_owner_id.
    let map_owner_id_2 = bpfman_output_map_owner_id(&binding_2);
    assert!(map_owner_id_2 == *(map_owner_id.as_ref().unwrap()));

    // Verify the "Map Pin Path:" is set properly.
    let map_pin_path_2 = bpfman_output_map_pin_path(&binding_2);
    assert!(map_pin_path_2 == map_pin_path);

    // Unload the Map Owner Program
    let result = bpfman_del_program(&(map_owner_id.unwrap()));
    assert!(result.is_ok());

    //drop(bpfman_guard);

    // Retrive the Program sharing the map
    let stdout_3 = bpfman_get(shared_owner_id.as_ref().unwrap());
    let binding_3 = stdout_3.unwrap();

    //let _bpfman_guard = start_bpfman().unwrap();

    // Verify "Map Used By:" field is set to only the
    // 2nd loaded program (one sharing the map).
    let map_used_by_3 = bpfman_output_xdp_map_used_by(&binding_3);
    assert!(map_used_by_3.len() == 1);
    assert!(map_used_by_3[0] == *(shared_owner_id.as_ref().unwrap()));

    // Unload the Map Sharing Program
    let result = bpfman_del_program(&(shared_owner_id.unwrap()));
    assert!(result.is_ok());
}

#[integration_test]
fn test_load_unload_tc() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    //let _bpfman_guard = start_bpfman().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

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
                DEFAULT_BPFMAN_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                &TC_PASS_IMAGE_LOC,
                TC_PASS_FILE_LOC,
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }
    assert_eq!(loaded_ids.len(), 10);

    // Verify TC filter is using correct priority
    let output = tc_filter_list(DEFAULT_BPFMAN_IFACE).unwrap();
    assert!(output.contains("pref 50"));
    assert!(output.contains("handle 0x2"));

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    let output = tc_filter_list(DEFAULT_BPFMAN_IFACE).unwrap();
    assert!(output.trim().is_empty());
}

#[integration_test]
fn test_load_unload_tracepoint() {
    debug!("Installing tracepoint programs");

    let globals = vec!["GLOBAL_u8=61", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let (prog_id, _) = add_tracepoint(
            Some(globals.clone()),
            lt,
            &TRACEPOINT_IMAGE_LOC,
            TRACEPOINT_FILE_LOC,
            TRACEPOINT_TRACEPOINT_NAME,
        );
        loaded_ids.push(prog_id.unwrap());
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_uprobe() {
    debug!("Installing uprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_uprobe(
            Some(globals.clone()),
            lt,
            &UPROBE_IMAGE_LOC,
            UPROBE_FILE_LOC,
            UPROBE_KERNEL_FUNCTION_NAME,
            UPROBE_TARGET,
            None, // container_pid
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_uretprobe() {
    debug!("Installing uretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_uretprobe(
            Some(globals.clone()),
            lt,
            &URETPROBE_IMAGE_LOC,
            URETPROBE_FILE_LOC,
            URETPROBE_FUNCTION_NAME,
            None, // target
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_kprobe() {
    debug!("Installing kprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_kprobe(
            Some(globals.clone()),
            lt,
            &KPROBE_IMAGE_LOC,
            KPROBE_FILE_LOC,
            KPROBE_KERNEL_FUNCTION_NAME,
            None, // container_pid
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_kretprobe() {
    debug!("Installing kretprobe program");

    let globals = vec!["GLOBAL_u8=63", "GLOBAL_u32=0D0C0B0A"];

    let mut loaded_ids: Vec<String> = vec![];

    // Load some kretprobes
    for lt in LOAD_TYPES {
        let prog_id = add_kretprobe(
            Some(globals.clone()),
            lt,
            &KRETPROBE_IMAGE_LOC,
            KRETPROBE_FILE_LOC,
            KRETPROBE_KERNEL_FUNCTION_NAME,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_pull_bytecode() {
    debug!("Pull bytecode image");

    // Just ensure this doesn't panic
    assert!(bpfman_pull_bytecode(&TRACEPOINT_IMAGE_LOC, Some(PULL_POLICY_ALWAYS), None).is_ok());
}

#[integration_test]
fn test_list_with_metadata() {
    let _namespace_guard = create_namespace().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

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
                DEFAULT_BPFMAN_IFACE,
                priority,
                Some(globals.clone()),
                Some(proceed_on.clone()),
                lt,
                &XDP_PASS_IMAGE_LOC,
                XDP_PASS_FILE_LOC,
                Some(XDP_PASS_NAME),
                None, // metadata
                None, // map_owner_id
            );
            loaded_ids.push(prog_id.unwrap());
        }
    }

    let key = "uuid=ITS_BPF_NOT_EBPF";
    let priority = rng.gen_range(1..255);
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        priority,
        Some(globals.clone()),
        Some(proceed_on.clone()),
        &LoadType::Image,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        Some(XDP_PASS_NAME),
        Some(vec![key]),
        None, // map_owner_id
    );
    let id = prog_id.unwrap();

    debug!("Listing programs with metadata {key}");
    // ensure listing with metadata works
    let list_output = bpfman_list(None, Some(vec![key])).unwrap();

    assert!(list_output.contains(&id));

    loaded_ids.push(id);

    assert_eq!(loaded_ids.len(), 5);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[integration_test]
fn test_load_unload_fentry() {
    debug!("Installing fentry program");

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_fentry_or_fexit(
            lt,
            &FENTRY_IMAGE_LOC,
            FENTRY_FILE_LOC,
            true,
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_load_unload_fexit() {
    debug!("Installing fexit program");

    let mut loaded_ids = vec![];

    for lt in LOAD_TYPES {
        let prog_id = add_fentry_or_fexit(
            lt,
            &FEXIT_IMAGE_LOC,
            FEXIT_FILE_LOC,
            false,
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME,
        )
        .unwrap();
        loaded_ids.push(prog_id);
    }

    verify_and_delete_programs(loaded_ids);
}
