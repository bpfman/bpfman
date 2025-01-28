/*

DISABLING THESE TESTS SINCE THEY NEED REWRITING TO USE THE BPFMAN LIBRARY NOT EXEC VIA CLI



use std::process::Command;

use crate::tests::{utils::*, RTDIR_FS_TC_EGRESS, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP};

const NONEXISTENT_UPROBE_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/uprobe_invalid:latest";
const NONEXISTENT_URETPROBE_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/uprobe_invalid.bpf.o";
const INVALID_XDP_IMAGE_LOC: &str = "quay.io//bpfman-bytecode/xdp_pass_invalid:latest";
const INVALID_XDP_FILE_LOC: &str = "tests//integration-test/bpf/.output/xdp_pass_invalid.bpf.o";
const NONEXISTENT_XDP_PASS_NAME: &str = "doesnotexist";
const INVALID_XDP_PASS_NAME: &str = "invalid/interface/%22erwt";
const NONEXISTENT_INTERFACE: &str = "eno1235";
const INVALID_INTERFACE: &str = "invalid/interface/%22erwt";

fn test_bpfmanlist() {
    let args = vec!["list"];

    assert!(!Command::cargo_bin("bpfman")
        .unwrap()
        .args(args)
        .ok()
        .expect("bpfman list failed")
        .stdout
        .is_empty());
}

fn common_load_parameter_testing() {
    for lt in LOAD_TYPES {
        println!(
            "Error checking common load parameters: non-existent {:?}",
            lt
        );
        let err_res = add_uprobe(
            None, // globals
            lt,
            NONEXISTENT_UPROBE_IMAGE_LOC,
            NONEXISTENT_URETPROBE_FILE_LOC,
            UPROBE_KERNEL_FUNCTION_NAME,
            UPROBE_TARGET,
            None, // container_pid
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }

    for lt in LOAD_TYPES {
        println!("Error checking common load parameters: invalid {:?}", lt);
        let err_res = add_tc(
            "ingress",
            DEFAULT_BPFMAN_IFACE,
            35,   // priority
            None, // globals
            None, // proceed_on
            lt,
            INVALID_XDP_IMAGE_LOC,
            INVALID_XDP_FILE_LOC,
            TC_PASS_NAME,
            None, // netns
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }

    println!("Error checking common load parameters: File non-existent name");
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        NONEXISTENT_XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking common load parameters: Image non-existent name");
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        NONEXISTENT_XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking common load parameters: invalid name");
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        INVALID_XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking common load parameters: invalid global");
    let invalid_globals = vec!["GLOBAL_u8=61,GLOBAL_u32=0D0C0B0A"];
    let err_res = add_tracepoint(
        Some(invalid_globals),
        &LoadType::File,
        &TRACEPOINT_IMAGE_LOC,
        TRACEPOINT_FILE_LOC,
        TRACEPOINT_TRACEPOINT_NAME,
        TRACEPOINT_NAME,
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking common load parameters: invalid metadata");
    let key = "invalid metadata";
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        Some(vec![key]), // metadata
        None,            // map_owner_id
        None,            // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking common load parameters: invalid map owner");
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        INVALID_INTEGER, // priority
        None,            // globals
        None,            // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

fn fentry_load_parameter_testing() {
    println!("Error checking Fentry load parameters: invalid function name");
    let err_res = add_fentry_or_fexit(
        &LoadType::Image,
        &FENTRY_IMAGE_LOC,
        FENTRY_FILE_LOC,
        true, // fentry
        "invalid",
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking Fentry load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_fentry_or_fexit(
            lt,
            &KPROBE_IMAGE_LOC,
            KPROBE_FILE_LOC,
            true, // fentry
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME,
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn fexit_load_parameter_testing() {
    println!("Error checking Fexit load parameters: invalid function name");
    let err_res = add_fentry_or_fexit(
        &LoadType::Image,
        &FENTRY_IMAGE_LOC,
        FENTRY_FILE_LOC,
        false, // fentry
        "invalid",
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking Fexit load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_fentry_or_fexit(
            lt,
            &TRACEPOINT_IMAGE_LOC,
            TRACEPOINT_FILE_LOC,
            false, // fentry
            FENTRY_FEXIT_KERNEL_FUNCTION_NAME,
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn kprobe_load_parameter_testing() {
    println!("Error checking kprobe load parameters: invalid function name");
    let err_res = add_kprobe(
        None, // globals
        &LoadType::Image,
        &KPROBE_IMAGE_LOC,
        KPROBE_FILE_LOC,
        "invalid", // fn_name
        None,      // container_pid
    );
    assert!(err_res.load_id.is_err());
    test_bpfmanlist();

    println!("Error checking kprobe load parameters: container_pid (not supported)");
    let err_res = add_kprobe(
        None, // globals
        &LoadType::Image,
        &KPROBE_IMAGE_LOC,
        KPROBE_FILE_LOC,
        KPROBE_KERNEL_FUNCTION_NAME,
        Some("12345"), // container_pid
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking kprobe load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_kprobe(
            None, // globals
            lt,
            &URETPROBE_IMAGE_LOC,
            URETPROBE_FILE_LOC,
            KPROBE_KERNEL_FUNCTION_NAME,
            None, // container_pid
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn kretprobe_load_parameter_testing() {
    println!("Error checking kretprobe load parameters: invalid function name");
    let err_res = add_kretprobe(
        None, // globals
        &LoadType::Image,
        &KRETPROBE_IMAGE_LOC,
        KRETPROBE_FILE_LOC,
        "invalid", // fn_name
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking kretprobe load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_kretprobe(
            None, // globals
            lt,
            &UPROBE_IMAGE_LOC,
            UPROBE_FILE_LOC,
            KRETPROBE_KERNEL_FUNCTION_NAME,
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn tc_load_parameter_testing() {
    println!("Error checking TC load parameters: invalid direction");
    let err_res = add_tc(
        "invalid",
        NONEXISTENT_INTERFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking TC load parameters: non-existent interface");
    let err_res = add_tc(
        "egress",
        NONEXISTENT_INTERFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking TC load parameters: invalid interface");
    let err_res = add_tc(
        "ingress",
        INVALID_INTERFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking TC load parameters: invalid priority");
    let err_res = add_tc(
        "egress",
        DEFAULT_BPFMAN_IFACE,
        INVALID_INTEGER, // priority
        None,            // globals
        None,            // proceed_on
        &LoadType::File,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking TC load parameters: invalid proceed-on");
    let proceed_on = vec!["redirect", "invalid_value"];
    let err_res = add_tc(
        "ingress",
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        Some(proceed_on.clone()),
        &LoadType::File,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    // Commented out due to Issue#1101
    //for lt in LOAD_TYPES {
    //    println!(
    //        "Error checking TC Ingress load parameters: {:?} load wrong program type",
    //        lt
    //    );
    //    let err_res = add_tc(
    //        "ingress",
    //        DEFAULT_BPFMAN_IFACE,
    //        35,   // priority
    //        None, // globals
    //        None, // proceed_on
    //        lt,
    //        XDP_PASS_IMAGE_LOC,
    //        XDP_PASS_FILE_LOC,
    //    );
    //    assert!(err_res.load_id.is_err());
    //    // Make sure bpfman is still accessible after command
    //    test_bpfmanlist();
    //}

    //for lt in LOAD_TYPES {
    //    println!(
    //        "Error checking TC Egress load parameters: {:?} load wrong program type",
    //        lt
    //    );
    //    let err_res = add_tc(
    //        "egress",
    //        DEFAULT_BPFMAN_IFACE,
    //        35,   // priority
    //        None, // globals
    //        None, // proceed_on
    //        lt,
    //        XDP_PASS_IMAGE_LOC,
    //        XDP_PASS_FILE_LOC,
    //    );
    //    assert!(err_res.load_id.is_err());
    //    // Make sure bpfman is still accessible after command
    //    test_bpfmanlist();
    //}
}

fn tracepoint_load_parameter_testing() {
    println!("Error checking tracepoint load parameters: non-existent tracepoint");
    let err_res = add_tracepoint(
        None, // globals
        &LoadType::Image,
        &TRACEPOINT_IMAGE_LOC,
        TRACEPOINT_FILE_LOC,
        "invalid", // tracepoint
        TRACEPOINT_NAME,
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking tracepoint load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_tracepoint(
            None, // globals
            lt,
            &FENTRY_IMAGE_LOC,
            FENTRY_FILE_LOC,
            TRACEPOINT_TRACEPOINT_NAME,
            TRACEPOINT_NAME,
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn uprobe_load_parameter_testing() {
    println!("Error checking uprobe load parameters: invalid function name");
    let err_res = add_uprobe(
        None, // globals
        &LoadType::Image,
        &UPROBE_IMAGE_LOC,
        UPROBE_FILE_LOC,
        "invalid",
        UPROBE_TARGET,
        None, // container_pid
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking uprobe load parameters: invalid container pid");
    let err_res = add_uprobe(
        None, // globals
        &LoadType::File,
        &KRETPROBE_IMAGE_LOC,
        KRETPROBE_FILE_LOC,
        UPROBE_KERNEL_FUNCTION_NAME,
        UPROBE_TARGET,
        Some("invalid"), // container_pid
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking uprobe load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_uprobe(
            None, // globals
            lt,
            &UPROBE_IMAGE_LOC,
            UPROBE_FILE_LOC,
            UPROBE_KERNEL_CONT_PID_FUNCTION_NAME,
            UPROBE_TARGET,
            None, // container_pid
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }

    println!("Error checking uprobe load parameters: invalid target");
    let container = start_container().unwrap();
    let _trace_guard = start_trace_pipe().unwrap();
    let container_pid = container.container_pid().to_string();
    let err_res = add_uprobe(
        None, // globals
        &LoadType::Image,
        &UPROBE_IMAGE_LOC,
        UPROBE_FILE_LOC,
        UPROBE_KERNEL_FUNCTION_NAME,
        "invalid",
        Some(&container_pid),
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

fn uretprobe_load_parameter_testing() {
    println!("Error checking uretprobe load parameters: invalid function name");
    let err_res = add_uretprobe(
        None, // globals
        &LoadType::Image,
        &URETPROBE_IMAGE_LOC,
        URETPROBE_FILE_LOC,
        "invalid",
        None,
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking uretprobe load parameters: invalid target");
    let err_res = add_uretprobe(
        None, // globals
        &LoadType::Image,
        &URETPROBE_IMAGE_LOC,
        URETPROBE_FILE_LOC,
        URETPROBE_FUNCTION_NAME,
        Some("invalid"),
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    for lt in LOAD_TYPES {
        println!(
            "Error checking uretprobe load parameters: {:?} load wrong program type",
            lt
        );
        let err_res = add_uretprobe(
            None, // globals
            lt,
            &KPROBE_IMAGE_LOC,
            KPROBE_FILE_LOC,
            URETPROBE_FUNCTION_NAME,
            None, // target
        );
        assert!(err_res.load_id.is_err());
        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
}

fn xdp_load_parameter_testing() {
    println!("Error checking XDP load parameters: non-existent interface");
    let err_res = add_xdp(
        NONEXISTENT_INTERFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking XDP load parameters: invalid interface");
    let err_res = add_xdp(
        INVALID_INTERFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking XDP load parameters: invalid priority");
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        INVALID_INTEGER, // priority
        None,            // globals
        None,            // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking XDP load parameters: invalid proceed-on");
    let proceed_on = vec!["drop", "invalid_value"];
    let err_res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        Some(proceed_on.clone()),
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    assert!(err_res.load_id.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    // Commented out due to Issue#1101
    //for lt in LOAD_TYPES {
    //    println!(
    //        "Error checking XDP load parameters: {:?} load wrong program type",
    //        lt
    //    );
    //    let err_res = add_xdp(
    //        DEFAULT_BPFMAN_IFACE,
    //        35,   // priority
    //        None, // globals
    //        None, // proceed_on
    //        lt,
    //        TC_PASS_IMAGE_LOC,
    //        TC_PASS_FILE_LOC,
    //        Some(XDP_PASS_NAME),
    //        None, // metadata
    //        None, // map_owner_id
    //    );
    //    assert!(err_res.load_id.is_err());
    //    // Make sure bpfman is still accessible after command
    //    test_bpfmanlist();
    //}
}

fn common_get_parameter_testing() {
    println!("Error checking get parameters: invalid program id");
    let output = bpfman_get("invalid");
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking get parameters: unused program id");
    let output = bpfman_get("999999");
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

fn common_list_parameter_testing() {
    println!("Error checking list parameters: invalid program type");
    let output = bpfman_list(Some("invalid_pt"), None);
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking list parameters: invalid metadata");
    let key = "invalid metadata";
    let output = bpfman_list(None, Some(vec![key]));
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

fn common_unload_parameter_testing() {
    println!("Error checking unload parameters: invalid program id");
    let output = bpfman_del_program("invalid");
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking unload parameters: unused program id");
    let output = bpfman_del_program("999999");
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

fn common_pull_parameter_testing() {
    println!("Error checking pull parameters: non-existent Image");
    let output = bpfman_pull_bytecode(NONEXISTENT_UPROBE_IMAGE_LOC, None, None);
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking pull parameters: invalid Image");
    let output = bpfman_pull_bytecode(INVALID_XDP_IMAGE_LOC, None, None);
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking pull parameters: invalid registry authority");
    let output = bpfman_pull_bytecode(&TRACEPOINT_IMAGE_LOC, None, Some("Invalid"));
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();

    println!("Error checking pull parameters: invalid pull policy");
    let output = bpfman_pull_bytecode(&TRACEPOINT_IMAGE_LOC, Some("Invalid"), None);
    assert!(output.is_err());
    // Make sure bpfman is still accessible after command
    test_bpfmanlist();
}

#[test]
fn test_invalid_parameters() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    // Install one set of XDP programs
    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing programs");
    let mut loaded_ids = vec![];
    for lt in LOAD_TYPES {
        let res = add_tracepoint(
            None, // globals
            lt,
            &TRACEPOINT_IMAGE_LOC,
            TRACEPOINT_FILE_LOC,
            TRACEPOINT_TRACEPOINT_NAME,
            TRACEPOINT_NAME,
        );

        if let Ok(id) = res.load_id {
            loaded_ids.push(id);
        }

        // Make sure bpfman is still accessible after command
        test_bpfmanlist();
    }
    assert_eq!(loaded_ids.len(), 2);

    /* Issue#1101 - Add dispatcher based programs - BEGIN */
    let res = add_xdp(
        DEFAULT_BPFMAN_IFACE,
        50,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::File,
        &XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        XDP_PASS_NAME,
        None, // metadata
        None, // map_owner_id
        None, // netns
    );
    if let Ok(id) = res.load_id {
        loaded_ids.push(id);
    }

    let res = add_tc(
        "ingress",
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::Image,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    if let Ok(id) = res.load_id {
        loaded_ids.push(id);
    }

    let res = add_tc(
        "egress",
        DEFAULT_BPFMAN_IFACE,
        35,   // priority
        None, // globals
        None, // proceed_on
        &LoadType::File,
        &TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
        TC_PASS_NAME,
        None, // netns
    );
    if let Ok(id) = res.load_id {
        loaded_ids.push(id);
    }
    /* Issue#1101 - Add dispatcher based programs - END */

    common_load_parameter_testing();
    fentry_load_parameter_testing();
    fexit_load_parameter_testing();
    kprobe_load_parameter_testing();
    kretprobe_load_parameter_testing();
    tc_load_parameter_testing();
    tracepoint_load_parameter_testing();
    uprobe_load_parameter_testing();
    uretprobe_load_parameter_testing();
    xdp_load_parameter_testing();

    common_get_parameter_testing();
    common_list_parameter_testing();
    common_unload_parameter_testing();
    common_pull_parameter_testing();

    // Cleanup Installed Programs
    verify_and_delete_programs(loaded_ids);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_EGRESS));
}*/
