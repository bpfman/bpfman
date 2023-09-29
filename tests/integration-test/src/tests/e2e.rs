use std::{path::PathBuf, thread::sleep, time::Duration};

use bpfd_api::util::directories::{RTDIR_FS_TC_EGRESS, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP};
use log::debug;

use super::{integration_test, IntegrationTest};
use crate::tests::utils::*;

const GLOBAL_1: &str = "GLOBAL_u8=25";
const GLOBAL_2: &str = "GLOBAL_u8=29";
const GLOBAL_3: &str = "GLOBAL_u8=2B";
const GLOBAL_4: &str = "GLOBAL_u8=35";
const GLOBAL_5: &str = "GLOBAL_u8=3B";
const GLOBAL_6: &str = "GLOBAL_u8=3D";

const XDP_GLOBAL_1_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x25";
const XDP_GLOBAL_2_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x29";
const XDP_GLOBAL_3_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x2B";
const TC_ING_GLOBAL_1_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x25";
const TC_ING_GLOBAL_2_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x29";
const TC_ING_GLOBAL_3_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x2B";
const TC_EG_GLOBAL_4_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x35";
const TC_EG_GLOBAL_5_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x3B";
const TC_EG_GLOBAL_6_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x3D";
const TRACEPOINT_GLOBAL_1_LOG: &str = "bpf_trace_printk:  TP: GLOBAL_u8: 0x25";
const UPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk:  UP: GLOBAL_u8: 0x25";
const URETPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk: URP: GLOBAL_u8: 0x25";
const KPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk:  KP: GLOBAL_u8: 0x25";
const KRETPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk: KRP: GLOBAL_u8: 0x25";

#[integration_test]
fn test_proceed_on_xdp() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();
    let _bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    let mut loaded_ids = vec![];

    debug!("Installing 1st xdp program");
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFD_IFACE,
        75,
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        None,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    // Make sure the first programs are running and logging
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));

    // Install a 2nd xdp program with a higher priority that doesn't proceed on
    // "pass", which this program will return.  This should prevent the first
    // program from being executed.
    debug!("Installing 2nd xdp program");
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_2, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["drop", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        None,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd program and not from the 1st program.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));

    // Install a 3rd xdp program with a higher priority that has proceed on
    // "pass", which this program will return.  We should see logs from the 2nd
    // and 3rd programs, but still not the first.
    debug!("Installing 3rd xdp program");
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_3, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["pass", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        None,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd & 3rd programs, but not from the 1st
    // program.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_3_LOG));
    debug!("Successfully completed xdp proceed-on test");

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_proceed_on_tc() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();
    let _bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    let mut loaded_ids = vec![];

    debug!("Installing 1st tc ingress program");
    let (prog_id, _) = add_tc(
        "ingress",
        DEFAULT_BPFD_IFACE,
        75,
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Installing 1st tc egress program");
    let (prog_id, _) = add_tc(
        "egress",
        DEFAULT_BPFD_IFACE,
        75,
        Some([GLOBAL_4, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    // Make sure the first programs are running and logging
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));

    // Install a 2nd tc program in each direction with a higher priority that
    // doesn't proceed on "ok", which this program will return.  We should see
    // logs from the 2nd programs, but still not the first.
    debug!("Installing 2nd tc ingress program");
    let (prog_id, _) = add_tc(
        "ingress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_2, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["shot", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Installing 2nd tc egress program");
    let (prog_id, _) = add_tc(
        "egress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_5, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["shot", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd programs, but not from the 1st
    // programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(!trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));

    // Install a 3rd tc program in each direction with a higher priority that
    // proceeds on "ok", which this program will return.  We should see logs
    // from the 2nd and 3rd programs, but still not the first.
    debug!("Installing 3rd tc ingress program");
    let (prog_id, _) = add_tc(
        "ingress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_3, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["ok", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Installing 3rd tc egress program");
    let (prog_id, _) = add_tc(
        "egress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_6, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        Some(["ok", "dispatcher_return"].to_vec()),
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );
    loaded_ids.push(prog_id.unwrap());

    debug!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd and 3rd TC programs, but not from the
    // 1st programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_3_LOG));
    debug!("Successfully completed tc ingress proceed-on test");
    assert!(!trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_6_LOG));
    debug!("Successfully completed tc egress proceed-on test");

    verify_and_delete_programs(loaded_ids);
}

#[integration_test]
fn test_program_execution_with_global_variables() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let _trace_guard = start_trace_pipe().unwrap();
    let _bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    let mut loaded_ids = vec![];

    debug!("Installing xdp program");
    let (prog_id, _) = add_xdp(
        DEFAULT_BPFD_IFACE,
        75,
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        XDP_PASS_IMAGE_LOC,
        XDP_PASS_FILE_LOC,
        None,
    );

    loaded_ids.push(prog_id.unwrap());

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    debug!("Installing tc ingress program");
    let (prog_id, _) = add_tc(
        "ingress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    debug!("Installing tc egress program");
    let (prog_id, _) = add_tc(
        "egress",
        DEFAULT_BPFD_IFACE,
        50,
        Some([GLOBAL_4, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        None,
        &LoadType::Image,
        TC_PASS_IMAGE_LOC,
        TC_PASS_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    assert!(bpffs_has_entries(RTDIR_FS_TC_EGRESS));

    debug!("Installing tracepoint program");
    let (prog_id, _) = add_tracepoint(
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        &LoadType::Image,
        TRACEPOINT_IMAGE_LOC,
        TRACEPOINT_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    debug!("Installing uprobe program");
    let prog_id = add_uprobe(
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        &LoadType::Image,
        UPROBE_IMAGE_LOC,
        UPROBE_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    debug!("Installing uretprobe program");
    let prog_id = add_uretprobe(
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        &LoadType::Image,
        URETPROBE_IMAGE_LOC,
        URETPROBE_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    debug!("Installing kprobe program");
    let prog_id = add_kprobe(
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        &LoadType::Image,
        KPROBE_IMAGE_LOC,
        KPROBE_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    debug!("Installing kretprobe program");
    let prog_id = add_kretprobe(
        Some([GLOBAL_1, "GLOBAL_u32=0A0B0C0D"].to_vec()),
        &LoadType::Image,
        KRETPROBE_IMAGE_LOC,
        KRETPROBE_FILE_LOC,
    );

    loaded_ids.push(prog_id.unwrap());

    debug!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    debug!("Successfully validated xdp global variable");
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    debug!("Successfully validated tc ingress global variable");
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    debug!("Successfully validated tc egress global variable");
    assert!(trace_pipe_log.contains(TRACEPOINT_GLOBAL_1_LOG));
    debug!("Successfully validated tracepoint global variable");
    assert!(trace_pipe_log.contains(KPROBE_GLOBAL_1_LOG));
    debug!("Successfully validated kprobe global variable");
    assert!(trace_pipe_log.contains(KRETPROBE_GLOBAL_1_LOG));
    debug!("Successfully validated kretprobe global variable");
    assert!(trace_pipe_log.contains(UPROBE_GLOBAL_1_LOG));
    debug!("Successfully validated uprobe global variable");
    assert!(trace_pipe_log.contains(URETPROBE_GLOBAL_1_LOG));
    debug!("Successfully validated uretprobe global variable");

    verify_and_delete_programs(loaded_ids);

    // Verify the bpffs is empty
    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_EGRESS));
}

#[integration_test]
fn test_load_unload_xdp_maps() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    debug!("Installing xdp_counter program");

    // Install an xdp counter program
    let (prog_id, map_pin_path) = add_xdp(
        DEFAULT_BPFD_IFACE,
        100,
        None,
        None,
        &LoadType::Image,
        XDP_COUNTER_IMAGE_LOC,
        "",
        None,
    );

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    debug!("Verify xdp_counter map pin directory was created, and maps were pinned");

    assert!(PathBuf::from(map_pin_path.unwrap())
        .join("xdp_stats_map")
        .exists());

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(vec![prog_id.unwrap()]);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[integration_test]
fn test_load_unload_tc_maps() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let bpfd_guard = start_bpfd().unwrap();

    assert!(iface_exists(DEFAULT_BPFD_IFACE));

    debug!("Installing tc_counter program");

    // Install an  counter program
    let (prog_id, map_pin_path) = add_tc(
        "ingress",
        DEFAULT_BPFD_IFACE,
        100,
        None,
        None,
        &LoadType::Image,
        TC_COUNTER_IMAGE_LOC,
        "",
    );

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    debug!("Verify tc_counter map pin directory was created, and maps were pinned");

    assert!(PathBuf::from(map_pin_path.unwrap())
        .join("tc_stats_map")
        .exists());

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(vec![prog_id.unwrap()]);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));
}

#[integration_test]
fn test_load_unload_tracepoint_maps() {
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let bpfd_guard = start_bpfd().unwrap();

    debug!("Installing tracepoint_counter program");

    let (prog_id, map_pin_path) =
        add_tracepoint(None, &LoadType::Image, TRACEPOINT_COUNTER_IMAGE_LOC, "");

    debug!("Verify tracepiont_counter map pin directory was created, and maps were pinned");

    assert!(PathBuf::from(map_pin_path.unwrap())
        .join("tracepoint_stats_map")
        .exists());

    // Verify rule persistence between restarts
    drop(bpfd_guard);
    let _bpfd_guard = start_bpfd().unwrap();

    verify_and_delete_programs(vec![prog_id.unwrap()]);
}
