use std::{collections::HashMap, iter, path::PathBuf};

use bpfman::{
    directories::RTDIR_FS_MAPS,
    list_programs, pull_bytecode, remove_program, setup,
    types::{
        AttachInfo, BytecodeImage, ImagePullPolicy, ListFilter, Location, TcProceedOn, XdpProceedOn,
    },
};
use rand::Rng;

use crate::tests::{
    RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP,
    e2e::{GLOBAL_U8, GLOBAL_U32},
    utils::*,
};

#[test]
fn test_load_unload_xdp() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing xdp_pass programs");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let proceed_on = XdpProceedOn::from_strings(vec![
        "aborted".to_string(),
        "drop".to_string(),
        "pass".to_string(),
        "tx".to_string(),
        "redirect".to_string(),
        "dispatcher_return".to_string(),
    ])
    .unwrap();

    let mut rng = rand::rng();

    // Install a few xdp programs
    let image = Location::Image(BytecodeImage {
        image_url: XDP_PASS_IMAGE_LOC.to_string(),
        image_pull_policy: ImagePullPolicy::Always,
        username: None,
        password: None,
    });
    let file = Location::File(XDP_PASS_FILE_LOC.to_string());

    let mut progs = vec![];
    for loc in iter::repeat_n(file.clone(), 5).chain(iter::repeat_n(image.clone(), 5)) {
        let prog = add_xdp(
            &config,
            &root_db,
            XDP_PASS_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Xdp {
                iface: DEFAULT_BPFMAN_IFACE.to_string(),
                priority: rng.random_range(1..255),
                proceed_on: proceed_on.clone(),
                netns: None,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    assert_eq!(progs.len(), 10);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    verify_and_delete_programs(&config, &root_db, progs);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[test]
fn test_map_sharing_load_unload_xdp() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let (config, root_db) = setup().unwrap();
    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    // Load first program, which will own the map.
    println!("Installing xdp_counter map owner program 1");
    let prog_1 = add_xdp(
        &config,
        &root_db,
        XDP_COUNTER_NAME.to_string(),
        Location::Image(BytecodeImage {
            image_url: XDP_COUNTER_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
        HashMap::new(),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            proceed_on: XdpProceedOn::default(),
            netns: None,
            metadata: HashMap::new(),
        },
    );

    // Verify "Map Used By:" field is set to only the just loaded program.
    let prog_1_id = prog_1.get_data().get_id().unwrap();
    assert_eq!(
        prog_1.get_data().get_maps_used_by().unwrap(),
        vec![prog_1_id]
    );

    // Verify the "Map Owner Id:" is None.
    assert!(prog_1.get_data().get_map_owner_id().unwrap().is_none());

    // Verify the "Map Pin Path:" is set properly.
    assert_eq!(
        prog_1.get_data().get_map_pin_path().unwrap(),
        Some(PathBuf::from(RTDIR_FS_MAPS).join(prog_1_id.to_string()))
    );

    // Load second program, which will share the map with the first program.
    println!("Installing xdp_counter map sharing program 2");
    let prog_2 = add_xdp(
        &config,
        &root_db,
        XDP_COUNTER_NAME.to_string(),
        Location::Image(BytecodeImage {
            image_url: XDP_COUNTER_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
        HashMap::new(),
        HashMap::new(),
        Some(prog_1_id),
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            proceed_on: XdpProceedOn::default(),
            netns: None,
            metadata: HashMap::new(),
        },
    );

    // Verify the "Map Used By:" field is set to both loaded program.
    // Order of programs is not guarenteed.
    let prog_2_id = prog_2.get_data().get_id().unwrap();

    assert!(
        prog_2
            .get_data()
            .get_maps_used_by()
            .unwrap()
            .contains(&prog_1_id)
    );
    assert!(
        prog_2
            .get_data()
            .get_maps_used_by()
            .unwrap()
            .contains(&prog_2_id)
    );

    // Verify the "Map Owner Id:" is set to the first program.
    assert_eq!(
        prog_2.get_data().get_map_owner_id().unwrap(),
        Some(prog_1_id)
    );

    // Ensure the map IDs for both programs are the same
    assert!(
        prog_1.get_data().get_kernel_map_ids().unwrap()
            == prog_2.get_data().get_kernel_map_ids().unwrap()
    );

    // Verify the "Map Pin Path:" is set properly.
    assert_eq!(
        prog_2.get_data().get_map_pin_path().unwrap(),
        Some(PathBuf::from(RTDIR_FS_MAPS).join(prog_1_id.to_string()))
    );

    // Unload the first program
    remove_program(&config, &root_db, prog_1_id).unwrap();

    // Verify the "Map Used By:" field is set to only the
    // 2nd loaded program (one sharing the map).
    assert_eq!(
        prog_2.get_data().get_maps_used_by().unwrap(),
        vec![prog_2_id]
    );

    remove_program(&config, &root_db, prog_2_id).unwrap();
}

#[test]
fn test_load_unload_tc() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing ingress tc programs");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let proceed_on = TcProceedOn::from_strings(vec![
        "unspec".to_string(),
        "ok".to_string(),
        "reclassify".to_string(),
        "shot".to_string(),
        "pipe".to_string(),
        "stolen".to_string(),
        "queued".to_string(),
        "repeat".to_string(),
        "redirect".to_string(),
        "trap".to_string(),
        "dispatcher_return".to_string(),
    ])
    .expect("Failed to create TcProceedOn");

    let mut rng = rand::rng();

    let file = Location::File(TC_PASS_FILE_LOC.to_string());
    let image = Location::Image(BytecodeImage {
        image_url: TC_PASS_IMAGE_LOC.to_string(),
        image_pull_policy: ImagePullPolicy::Always,
        username: None,
        password: None,
    });

    let mut progs = vec![];
    for loc in iter::repeat_n(file.clone(), 5).chain(iter::repeat_n(image.clone(), 5)) {
        let prog = add_tc(
            &config,
            &root_db,
            TC_PASS_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Tc {
                iface: DEFAULT_BPFMAN_IFACE.to_string(),
                priority: rng.random_range(1..255),
                direction: "ingress".to_string(),
                proceed_on: proceed_on.clone(),
                netns: None,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    assert_eq!(progs.len(), 10);

    // Verify TC filter is using correct priority
    let output = tc_filter_list(DEFAULT_BPFMAN_IFACE).unwrap();
    assert!(output.contains("pref 50"));
    assert!(output.contains("handle 0x2"));

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    verify_and_delete_programs(&config, &root_db, progs);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    let output = tc_filter_list(DEFAULT_BPFMAN_IFACE).unwrap();
    assert!(output.trim().is_empty());
}

#[test]
fn test_load_unload_tracepoint() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing tracepoint programs");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(TRACEPOINT_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: TRACEPOINT_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_tracepoint(
            &config,
            &root_db,
            TRACEPOINT_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Tracepoint {
                tracepoint: TRACEPOINT_TRACEPOINT_NAME.to_string(),
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_uprobe() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing uprobe program");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(UPROBE_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: UPROBE_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_uprobe(
            &config,
            &root_db,
            UPROBE_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Uprobe {
                fn_name: Some(UPROBE_FUNCTION_NAME.to_string()),
                offset: 0,
                target: UPROBE_TARGET.to_string(),
                metadata: HashMap::new(),
                container_pid: None,
                pid: None,
            },
        );
        progs.push(prog);
    }

    // We're not testing the actual program here, but need to call the
    // function at least once in the test code to ensure it's not
    // optimized out.
    trigger_bpf_program();

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_uretprobe() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing uretprobe program");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(URETPROBE_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: URETPROBE_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_uprobe(
            &config,
            &root_db,
            URETPROBE_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Uprobe {
                fn_name: Some(URETPROBE_FUNCTION_NAME.to_string()),
                offset: 0,
                target: URETPROBE_TARGET.to_string(),
                metadata: HashMap::new(),
                container_pid: None,
                pid: None,
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_kprobe() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing kprobe program");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(KPROBE_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: KPROBE_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_kprobe(
            &config,
            &root_db,
            KPROBE_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Kprobe {
                fn_name: KPROBE_KERNEL_FUNCTION_NAME.to_string(),
                offset: 0,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_kretprobe() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing kretprobe program");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(KRETPROBE_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: KRETPROBE_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_kprobe(
            &config,
            &root_db,
            KRETPROBE_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Kprobe {
                fn_name: KRETPROBE_KERNEL_FUNCTION_NAME.to_string(),
                offset: 0,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_pull_bytecode() {
    init_logger();
    let (_, root_db) = setup().unwrap();
    println!("Pull bytecode image");

    pull_bytecode(
        &root_db,
        BytecodeImage {
            image_url: TRACEPOINT_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        },
    )
    .unwrap();
}

#[test]
fn test_list_with_metadata() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing xdp_pass programs");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let proceed_on = XdpProceedOn::from_strings(vec![
        "aborted".to_string(),
        "drop".to_string(),
        "pass".to_string(),
        "tx".to_string(),
        "redirect".to_string(),
        "dispatcher_return".to_string(),
    ])
    .unwrap();

    let mut rng = rand::rng();

    // Install a few xdp programs
    let image = Location::Image(BytecodeImage {
        image_url: XDP_PASS_IMAGE_LOC.to_string(),
        image_pull_policy: ImagePullPolicy::Always,
        username: None,
        password: None,
    });

    let loads = iter::repeat_n(image.clone(), 2);

    let mut progs = vec![];
    for loc in loads {
        let prog = add_xdp(
            &config,
            &root_db,
            XDP_PASS_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Xdp {
                iface: DEFAULT_BPFMAN_IFACE.to_string(),
                priority: rng.random_range(1..255),
                proceed_on: proceed_on.clone(),
                netns: None,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    assert_eq!(progs.len(), 2);

    let meta_key = "uuid".to_string();
    let meta_val = "ITS_BPF_NOT_EBPF".to_string();
    let prog = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        image,
        globals.clone(),
        HashMap::from([(meta_key.clone(), meta_val.clone())]),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: rng.random_range(1..255),
            proceed_on: proceed_on.clone(),
            netns: None,
            metadata: HashMap::new(),
        },
    );
    let id = prog.get_data().get_id().unwrap();
    progs.push(prog);

    println!("Listing programs with metadata {meta_key}={meta_val}");

    let selector = HashMap::from([(meta_key.clone(), meta_val.clone())]);
    let res: Vec<u32> = list_programs(&root_db, ListFilter::new(None, selector, false))
        .unwrap()
        .into_iter()
        .map(|p| p.get_data().get_id().unwrap())
        .collect();

    assert!(res.contains(&id));

    assert_eq!(progs.len(), 3);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    verify_and_delete_programs(&config, &root_db, progs);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[test]
fn test_load_unload_fentry() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing fentry program");

    let mut progs = vec![];

    for loc in [
        Location::File(FENTRY_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: FENTRY_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_fentry(
            &config,
            &root_db,
            FENTRY_NAME.to_string(),
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME.to_string(),
            loc,
            HashMap::new(),
            HashMap::new(),
            None,
            AttachInfo::Fentry {
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_fexit() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    println!("Installing fexit program");

    let mut progs = vec![];

    for loc in [
        Location::File(FEXIT_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: FEXIT_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_fexit(
            &config,
            &root_db,
            FEXIT_NAME.to_string(),
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME.to_string(),
            loc,
            HashMap::new(),
            HashMap::new(),
            None,
            AttachInfo::Fexit {
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_load_unload_cosign_disabled() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _cosign_guard = disable_cosign();

    println!("Installing kprobe program with cosign disabled");

    let globals = HashMap::from([
        (GLOBAL_U8.to_string(), vec![0x61]),
        (GLOBAL_U32.to_string(), vec![0x0D, 0x0C, 0x0B, 0x0A]),
    ]);

    let mut progs = vec![];

    for loc in [
        Location::File(KPROBE_FILE_LOC.to_string()),
        Location::Image(BytecodeImage {
            image_url: KPROBE_IMAGE_LOC.to_string(),
            image_pull_policy: ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
    ] {
        let prog = add_kprobe(
            &config,
            &root_db,
            KPROBE_NAME.to_string(),
            loc,
            globals.clone(),
            HashMap::new(),
            None,
            AttachInfo::Kprobe {
                fn_name: KPROBE_KERNEL_FUNCTION_NAME.to_string(),
                offset: 0,
                metadata: HashMap::new(),
            },
        );
        progs.push(prog);
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[unsafe(no_mangle)]
#[inline(never)]
pub extern "C" fn trigger_bpf_program() {
    core::hint::black_box(trigger_bpf_program);
}
