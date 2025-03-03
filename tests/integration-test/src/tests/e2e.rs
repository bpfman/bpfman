use std::{collections::HashMap, path::PathBuf, thread::sleep, time::Duration};

use bpfman::{
    remove_program, setup,
    types::{AttachInfo, BytecodeImage, Location, TcProceedOn, XdpProceedOn},
};
use procfs::sys::kernel::Version;

use crate::tests::{
    basic::trigger_bpf_program, utils::*, RTDIR_FS_TC_EGRESS, RTDIR_FS_TC_INGRESS, RTDIR_FS_XDP,
};

pub(crate) const GLOBAL_U8: &str = "GLOBAL_u8";
pub(crate) const GLOBAL_U32: &str = "GLOBAL_u32";

const GLOBAL_1: u8 = 0x25;
const GLOBAL_2: u8 = 0x29;
const GLOBAL_3: u8 = 0x2B;
const GLOBAL_4: u8 = 0x35;
const GLOBAL_5: u8 = 0x3B;
const GLOBAL_6: u8 = 0x3D;
const GLOBAL_7: u8 = 0x3F;
const GLOBAL_8: u8 = 0x41;

const XDP_GLOBAL_1_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x25";
const XDP_GLOBAL_2_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x29";
const XDP_GLOBAL_3_LOG: &str = "bpf_trace_printk: XDP: GLOBAL_u8: 0x2B";
const TC_ING_GLOBAL_1_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x25";
const TC_ING_GLOBAL_2_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x29";
const TC_ING_GLOBAL_3_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x2B";
const TC_EG_GLOBAL_4_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x35";
const TC_EG_GLOBAL_5_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x3B";
const TC_EG_GLOBAL_6_LOG: &str = "bpf_trace_printk:  TC: GLOBAL_u8: 0x3D";
const TCX_GLOBAL_1_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x25";
const TCX_GLOBAL_2_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x29";
const TCX_GLOBAL_3_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x2B";
const TCX_GLOBAL_4_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x35";
const TCX_GLOBAL_5_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x3B";
const TCX_GLOBAL_6_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x3D";
const TCX_GLOBAL_7_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x3F";
const TCX_GLOBAL_8_LOG: &str = "bpf_trace_printk:  TCX: GLOBAL_u8: 0x41";
const TRACEPOINT_GLOBAL_1_LOG: &str = "bpf_trace_printk:  TP: GLOBAL_u8: 0x25";
const UPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk:  UP: GLOBAL_u8: 0x25";
const URETPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk: URP: GLOBAL_u8: 0x25";
const KPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk:  KP: GLOBAL_u8: 0x25";
const KRETPROBE_GLOBAL_1_LOG: &str = "bpf_trace_printk: KRP: GLOBAL_u8: 0x25";

#[test]
fn test_proceed_on_xdp() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    println!("Installing 1st xdp program");

    let prog1 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 75,
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            proceed_on: XdpProceedOn::default(),
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(prog1);

    println!("wait for some traffic to generate logs...");
    // This is gross. We should probably add some sort of retry mechanism to
    // handle checking for logs.
    sleep(Duration::from_secs(10));

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
    println!("Installing 2nd xdp program");

    let prog2 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 50,
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            proceed_on: XdpProceedOn::from_strings(vec![
                "drop".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(prog2);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd program and not from the 1st program.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));

    // Install a 3rd xdp program with a higher priority that has proceed on
    // "pass", which this program will return.  We should see logs from the 2nd
    // and 3rd programs, but still not the first.
    println!("Installing 3rd xdp program");
    let prog3 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_3]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 25,
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            proceed_on: XdpProceedOn::from_strings(vec![
                "pass".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(prog3);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd & 3rd programs, but not from the 1st
    // program.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_3_LOG));
    println!("Successfully completed xdp proceed-on test");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_unload_xdp() {
    init_logger();
    // This test confirms that after unloading a high priority program, the
    // proceedon configuration still works.  This test reproduces the case that
    // produced the xdp unload issue described in
    // https://github.com/bpfman/bpfman/issues/791
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    // Install the first lowest priority program.
    println!("Installing 1st xdp program");
    let prog1 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 75,
            proceed_on: XdpProceedOn::default(),
            metadata: HashMap::new(),
            netns: None,
        },
    );
    progs.push(prog1);

    // Install a 2nd xdp program with a higher priority than the first that has
    // proceed on "pass", which this program will return.
    println!("Installing 2nd xdp program");

    let prog2 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            proceed_on: XdpProceedOn::from_strings(vec![
                "pass".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
            metadata: HashMap::new(),
            netns: None,
        },
    );
    progs.push(prog2);

    // Install a 3rd xdp program with a higher priority than the second that has
    // proceed on "pass", which this program will return.
    println!("Installing 3rd xdp program");
    let prog3 = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_3]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 25,
            proceed_on: XdpProceedOn::from_strings(vec![
                "pass".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
            metadata: HashMap::new(),
            netns: None,
        },
    );

    // Don't save this id because we're going to unload it explicitly below.

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from all 3 programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_3_LOG));
    println!("All three logs are found");

    // Now delete the highest priority program and confirm that the other two
    // are still running.

    let result = remove_program(&config, &root_db, prog3.get_data().get_id().unwrap());
    assert!(result.is_ok());

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the first two programs, but not the 3rd.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(XDP_GLOBAL_2_LOG));
    assert!(!trace_pipe_log.contains(XDP_GLOBAL_3_LOG));
    println!("Successfully completed the xdp unload test");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_proceed_on_tc() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    println!("Installing 1st tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 75,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    println!("Installing 1st tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_4]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 75,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    println!("wait for some traffic to generate logs...");
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
    println!("Installing 2nd tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "shot".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    println!("Installing 2nd tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_5]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "shot".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
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
    println!("Installing 3rd tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_3]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 25,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    println!("Installing 3rd tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_6]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 25,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd and 3rd TC programs, but not from the
    // 1st programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_3_LOG));
    println!("Successfully completed tc ingress proceed-on test");
    assert!(!trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_6_LOG));
    println!("Successfully completed tc egress proceed-on test");

    // Make sure it still works like it did before we stopped and restarted bpfman
    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 2nd and 3rd TC programs, but not from the
    // 1st programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_3_LOG));
    println!("Successfully completed tc ingress proceed-on test");
    assert!(!trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_6_LOG));
    println!("Successfully completed tc egress proceed-on test");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_unload_tc() {
    init_logger();
    // This test confirms that after unloading a high priority program, the
    // proceedon configuration still works.  This test reproduces the case that
    // produced the tc unload issue described in
    // https://github.com/bpfman/bpfman/issues/791
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    // Install the first lowest priority programs.
    println!("Installing 1st tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 75,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    println!("Installing 1st tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_4]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 75,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    // Install a 2nd tc program in each direction with a higher priority than
    // the first that proceeds on "ok", which this program will return.
    println!("Installing 2nd tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    println!("Installing 2nd tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_5]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );
    progs.push(res);

    // Install a 3rd tc program in each direction with a higher priority than
    // the second that proceeds on "ok", which this program will return.
    println!("Installing 3rd tc ingress program");
    let ing_hi_pri_res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_3]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 25,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );

    println!("Installing 3rd tc egress program");
    let eg_hi_pri_res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_6]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 25,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::from_strings(vec![
                "ok".to_string(),
                "dispatcher_return".to_string(),
            ])
            .unwrap(),
        },
    );

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from all 3 programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_3_LOG));
    println!("All 3 tc ingress logs found");
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_6_LOG));
    println!("All 3 tc egress logs found");

    // Unload the 3rd programs
    let result = remove_program(
        &config,
        &root_db,
        ing_hi_pri_res.get_data().get_id().unwrap(),
    );
    assert!(result.is_ok());
    let result = remove_program(
        &config,
        &root_db,
        eg_hi_pri_res.get_data().get_id().unwrap(),
    );
    assert!(result.is_ok());

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the first 2 programs, but not the 3rd.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_2_LOG));
    assert!(!trace_pipe_log.contains(TC_ING_GLOBAL_3_LOG));
    println!("Successfully completed tc ingress unload test");
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_5_LOG));
    assert!(!trace_pipe_log.contains(TC_EG_GLOBAL_6_LOG));
    println!("Successfully completed tc egress unload test");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_program_execution_with_global_variables() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let _trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    println!("Installing xdp program");
    let res = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 75,
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            proceed_on: XdpProceedOn::default(),
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    println!("Installing tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    println!("Installing tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_4]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);
    assert!(bpffs_has_entries(RTDIR_FS_TC_EGRESS));

    println!("Installing tracepoint program");
    let res = add_tracepoint(
        &config,
        &root_db,
        TRACEPOINT_NAME.to_string(),
        Location::File(TRACEPOINT_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tracepoint {
            tracepoint: TRACEPOINT_TRACEPOINT_NAME.to_string(),
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing uprobe program");
    let res = add_uprobe(
        &config,
        &root_db,
        UPROBE_NAME.to_string(),
        Location::File(UPROBE_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Uprobe {
            fn_name: Some(UPROBE_FUNCTION_NAME.to_string()),
            offset: 0,
            target: UPROBE_TARGET.to_string(),
            pid: None,
            container_pid: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing uretprobe program");
    let res = add_uprobe(
        &config,
        &root_db,
        URETPROBE_NAME.to_string(),
        Location::File(URETPROBE_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Uprobe {
            fn_name: Some(URETPROBE_FUNCTION_NAME.to_string()),
            offset: 0,
            target: URETPROBE_TARGET.to_string(),
            pid: None,
            container_pid: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing kprobe program");
    let res = add_kprobe(
        &config,
        &root_db,
        KPROBE_NAME.to_string(),
        Location::File(KPROBE_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Kprobe {
            fn_name: KPROBE_KERNEL_FUNCTION_NAME.to_string(),
            metadata: HashMap::new(),
            container_pid: None,
            offset: 0,
        },
    );
    progs.push(res);

    println!("Installing kretprobe program");
    let res = add_kprobe(
        &config,
        &root_db,
        KRETPROBE_NAME.to_string(),
        Location::File(KRETPROBE_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Kprobe {
            fn_name: KRETPROBE_KERNEL_FUNCTION_NAME.to_string(),
            metadata: HashMap::new(),
            container_pid: None,
            offset: 0,
        },
    );
    progs.push(res);

    println!("wait for some traffic to generate logs...");
    trigger_bpf_program();
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    println!("Successfully validated xdp global variable");
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    println!("Successfully validated tc ingress global variable");
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    println!("Successfully validated tc egress global variable");
    assert!(trace_pipe_log.contains(TRACEPOINT_GLOBAL_1_LOG));
    println!("Successfully validated tracepoint global variable");
    assert!(trace_pipe_log.contains(KPROBE_GLOBAL_1_LOG));
    println!("Successfully validated kprobe global variable");
    assert!(trace_pipe_log.contains(KRETPROBE_GLOBAL_1_LOG));
    println!("Successfully validated kretprobe global variable");
    assert!(trace_pipe_log.contains(UPROBE_GLOBAL_1_LOG));
    println!("Successfully validated uprobe global variable");
    assert!(trace_pipe_log.contains(URETPROBE_GLOBAL_1_LOG));
    println!("Successfully validated uretprobe global variable");

    verify_and_delete_programs(&config, &root_db, progs);

    // Verify the bpffs is empty
    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));
    assert!(!bpffs_has_entries(RTDIR_FS_TC_EGRESS));
}

#[test]
fn test_load_unload_xdp_maps() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing xdp_counter program");

    // Install an xdp counter program
    let res = add_xdp(
        &config,
        &root_db,
        XDP_COUNTER_NAME.to_string(),
        Location::Image(BytecodeImage {
            image_url: XDP_COUNTER_IMAGE_LOC.to_string(),
            image_pull_policy: bpfman::types::ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
        HashMap::new(),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 100,
            proceed_on: XdpProceedOn::default(),
            metadata: HashMap::new(),
            netns: None,
        },
    );
    assert!(bpffs_has_entries(RTDIR_FS_XDP));

    println!("Verify xdp_counter map pin directory was created, and maps were pinned");
    let map_pin_path = res.get_data().get_map_pin_path().unwrap().unwrap();
    assert!(map_pin_path.join("xdp_stats_map").exists());

    verify_and_delete_programs(&config, &root_db, vec![res]);

    assert!(!bpffs_has_entries(RTDIR_FS_XDP));
}

#[test]
fn test_load_unload_tc_maps() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    println!("Installing tc_counter program");

    // Install an  counter program
    let res = add_tc(
        &config,
        &root_db,
        TC_COUNTER_NAME.to_string(),
        Location::Image(BytecodeImage {
            image_url: TC_COUNTER_IMAGE_LOC.to_string(),
            image_pull_policy: bpfman::types::ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
        HashMap::new(),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 100,
            netns: None,
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );

    assert!(bpffs_has_entries(RTDIR_FS_TC_INGRESS));

    println!("Verify tc_counter map pin directory was created, and maps were pinned");

    let map_pin_path = res.get_data().get_map_pin_path().unwrap().unwrap();
    assert!(map_pin_path.join("tc_stats_map").exists());

    verify_and_delete_programs(&config, &root_db, vec![res]);

    assert!(!bpffs_has_entries(RTDIR_FS_TC_INGRESS));
}

#[test]
fn test_load_unload_tracepoint_maps() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();

    println!("Installing tracepoint_counter program");

    let res = add_tracepoint(
        &config,
        &root_db,
        TRACEPOINT_COUNTER_NAME.to_string(),
        Location::Image(BytecodeImage {
            image_url: TRACEPOINT_COUNTER_IMAGE_LOC.to_string(),
            image_pull_policy: bpfman::types::ImagePullPolicy::Always,
            username: None,
            password: None,
        }),
        HashMap::new(),
        HashMap::new(),
        None,
        AttachInfo::Tracepoint {
            tracepoint: TRACEPOINT_TRACEPOINT_NAME.to_string(),
            metadata: HashMap::new(),
        },
    );

    println!("Verify tracepoint_counter map pin directory was created, and maps were pinned");

    let map_pin_path = res.get_data().get_map_pin_path().unwrap().unwrap();
    assert!(map_pin_path.join("tracepoint_stats_map").exists());

    verify_and_delete_programs(&config, &root_db, vec![res]);
}

#[test]
fn test_uprobe_container() {
    init_logger();
    let (config, root_db) = setup().unwrap();
    // Start docker container and verify we can attach a uprobe inside.
    let container = start_container().unwrap();
    let _trace_guard = start_trace_pipe().unwrap();

    let mut progs = vec![];

    let container_pid = container.container_pid();

    println!("Installing uprobe program");
    let res = add_uprobe(
        &config,
        &root_db,
        UPROBE_NAME.to_string(),
        Location::File(UPROBE_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Uprobe {
            fn_name: Some(UPROBE_CONTAINER_FUNCTION_NAME.to_string()),
            offset: 0,
            target: UPROBE_CONTAINER_TARGET.to_string(),
            pid: None,
            container_pid: Some(container_pid),
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    // run the target progream in the container to generate some logs
    container.bash(b"echo hello\necho ebpf is cool\necho goodbye");

    std::thread::sleep(std::time::Duration::from_secs(2));
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    println!("trace_pipe_log: {}", trace_pipe_log);
    assert!(trace_pipe_log.contains(UPROBE_GLOBAL_1_LOG));
    println!("Successfully validated uprobe in a container");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_tcx() {
    init_logger();
    // Check kernel version.  If it's less than 6.6, skip this test.
    let kernel_version = Version::current().unwrap();
    if kernel_version < Version::new(6, 6, 0) {
        println!("The kernel version is: {:?}", kernel_version);
        println!("Skipping tcx test.  Kernel must be at least 6.6 to support tcx.");
        return;
    }
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    // Install a tcx pass program in each direction at priority 1000, which is
    // the lowest priority.  We should see logs from both programs.
    println!("Installing 1st tcx ingress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_PASS_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 1000,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing 1st tcx egress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_PASS_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 1000,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    // Make sure the first programs are running and logging
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(TCX_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_2_LOG));
    println!("Successfully completed tcx_test test case #1");

    // Install a 2nd tcx program in each direction at a higher priority than the
    // first programs that returns TCX_NEXT. We should see logs from both sets
    // of programs.
    println!("Installing 2nd tcx ingress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_NEXT_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_3]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 100,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing 2nd tcx egress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_NEXT_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_4]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 100,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from both sets of tcx programs.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(TCX_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_2_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_3_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_4_LOG));
    println!("Successfully completed tcx_test test case #2");

    // Install a 3rd tcx program in each direction which returns TCX_DROP at a
    // higher priority than the existing programs. In this case, we should see
    // logs from the egress txc program #3 for the pings being sent from the
    // host, however, that txc program should drop the pings so the network
    // namespace should not respond.  We therefore will probably not see logs
    // from the ingress txc program #3 unless it is sending traffic for some
    // other reason.
    println!("Installing 3rd tcx ingress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_DROP_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_5]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing 3rd tcx egress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_DROP_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_6]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 50,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 3rd egress TCX program, but not from the
    // others.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_1_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_2_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_3_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_4_LOG));
    // don't check for ingress logs here (TCX_GLOBAL_5_LOG) because we may or
    // may not see any since the egress is dropping the pings.
    assert!(trace_pipe_log.contains(TCX_GLOBAL_6_LOG));
    println!("Successfully completed tcx_test test case #3");

    // Install a 4th tcx program in each direction that returns TCX_PASS at the
    // highest priority. We should see logs from the 4th set of programs, but
    // not the others.
    println!("Installing 4th tcx ingress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_PASS_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_7]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "ingress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 1,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing 4th tcx egress program");
    let res = add_tcx(
        &config,
        &root_db,
        TCX_TEST_PASS_NAME.to_string(),
        Location::File(TCX_TEST_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_8]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tcx {
            direction: "egress".to_string(),
            iface: DEFAULT_BPFMAN_IFACE.to_string(),
            priority: 1,
            netns: None,
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Clear the trace_pipe_log");
    drop(trace_guard);
    let _trace_guard = start_trace_pipe().unwrap();

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    // Make sure we have logs from the 4th set of TCX programs, but not from the
    // others.
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_1_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_2_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_3_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_4_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_5_LOG));
    assert!(!trace_pipe_log.contains(TCX_GLOBAL_6_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_7_LOG));
    assert!(trace_pipe_log.contains(TCX_GLOBAL_8_LOG));
    println!("Successfully completed all 4 tcx_test test cases");

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_netns() {
    init_logger();
    let kernel_version = Version::current().unwrap();
    let do_tcx = if kernel_version >= Version::new(6, 6, 0) {
        true
    } else {
        println!("The kernel version is: {:?}", kernel_version);
        println!("Skipping tcx test.  Kernel must be at least 6.6 to support tcx.");
        false
    };
    let (config, root_db) = setup().unwrap();
    let _namespace_guard = create_namespace().unwrap();
    let _ping_guard = start_ping().unwrap();
    let _trace_guard = start_trace_pipe().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    println!("Installing xdp program");
    let res = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 75,
            iface: NS_VETH.to_string(),
            proceed_on: XdpProceedOn::default(),
            netns: Some(PathBuf::from(NS_PATH)),
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: NS_VETH.to_string(),
            priority: 75,
            netns: Some(PathBuf::from(NS_PATH)),
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    println!("Installing tc egress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_4]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "egress".to_string(),
            iface: NS_VETH.to_string(),
            priority: 75,
            netns: Some(PathBuf::from(NS_PATH)),
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    if do_tcx {
        // Install a tcx pass program in each direction at priority 1000, which is
        // the lowest priority.  We should see logs from both programs.
        println!("Installing tcx ingress program");
        let res = add_tcx(
            &config,
            &root_db,
            TCX_TEST_PASS_NAME.to_string(),
            Location::File(TCX_TEST_FILE_LOC.to_string()),
            HashMap::from([
                (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
                (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
            ]),
            HashMap::new(),
            None,
            AttachInfo::Tcx {
                direction: "ingress".to_string(),
                iface: NS_VETH.to_string(),
                priority: 1000,
                netns: Some(PathBuf::from(NS_PATH)),
                metadata: HashMap::new(),
            },
        );
        progs.push(res);

        println!("Installing tcx egress program");
        let res = add_tcx(
            &config,
            &root_db,
            TCX_TEST_PASS_NAME.to_string(),
            Location::File(TCX_TEST_FILE_LOC.to_string()),
            HashMap::from([
                (GLOBAL_U8.to_string(), vec![GLOBAL_2]),
                (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
            ]),
            HashMap::new(),
            None,
            AttachInfo::Tcx {
                direction: "egress".to_string(),
                iface: NS_VETH.to_string(),
                priority: 1000,
                netns: Some(PathBuf::from(NS_PATH)),
                metadata: HashMap::new(),
            },
        );
        progs.push(res);
    }

    println!("wait for some traffic to generate logs...");
    sleep(Duration::from_secs(2));

    let ping_log = read_ping_log().unwrap();
    // Make sure we've had some pings
    assert!(ping_log.lines().count() > 2);

    // Make sure the programs are running and logging
    let trace_pipe_log = read_trace_pipe_log().unwrap();
    assert!(!trace_pipe_log.is_empty());
    assert!(trace_pipe_log.contains(XDP_GLOBAL_1_LOG));
    println!("xdp netns test program is working");
    assert!(trace_pipe_log.contains(TC_ING_GLOBAL_1_LOG));
    assert!(trace_pipe_log.contains(TC_EG_GLOBAL_4_LOG));
    println!("tc netns test programs are working");

    if do_tcx {
        assert!(trace_pipe_log.contains(TCX_GLOBAL_1_LOG));
        assert!(trace_pipe_log.contains(TCX_GLOBAL_2_LOG));
        println!("tcx netns test programs are working");
    }

    verify_and_delete_programs(&config, &root_db, progs);
}

#[test]
fn test_netns_delete() {
    init_logger();
    let kernel_version = Version::current().unwrap();
    let do_tcx = if kernel_version >= Version::new(6, 6, 0) {
        true
    } else {
        println!("The kernel version is: {:?}", kernel_version);
        println!("Skipping tcx test.  Kernel must be at least 6.6 to support tcx.");
        false
    };

    let (config, root_db) = setup().unwrap();
    let namespace_guard = create_namespace().unwrap();

    assert!(iface_exists(DEFAULT_BPFMAN_IFACE));

    let mut progs = vec![];

    println!("Installing xdp program");
    let res = add_xdp(
        &config,
        &root_db,
        XDP_PASS_NAME.to_string(),
        Location::File(XDP_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Xdp {
            priority: 75,
            iface: NS_VETH.to_string(),
            proceed_on: XdpProceedOn::default(),
            netns: Some(PathBuf::from(NS_PATH)),
            metadata: HashMap::new(),
        },
    );
    progs.push(res);

    println!("Installing tc ingress program");
    let res = add_tc(
        &config,
        &root_db,
        TC_PASS_NAME.to_string(),
        Location::File(TC_PASS_FILE_LOC.to_string()),
        HashMap::from([
            (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
            (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
        ]),
        HashMap::new(),
        None,
        AttachInfo::Tc {
            direction: "ingress".to_string(),
            iface: NS_VETH.to_string(),
            priority: 75,
            netns: Some(PathBuf::from(NS_PATH)),
            metadata: HashMap::new(),
            proceed_on: TcProceedOn::default(),
        },
    );
    progs.push(res);

    if do_tcx {
        // Install a tcx pass program in each direction at priority 1000, which is
        // the lowest priority.  We should see logs from both programs.
        println!("Installing tcx ingress program");
        let res = add_tcx(
            &config,
            &root_db,
            TCX_TEST_PASS_NAME.to_string(),
            Location::File(TCX_TEST_FILE_LOC.to_string()),
            HashMap::from([
                (GLOBAL_U8.to_string(), vec![GLOBAL_1]),
                (GLOBAL_U32.to_string(), vec![0x0A, 0x0B, 0x0C, 0x0D]),
            ]),
            HashMap::new(),
            None,
            AttachInfo::Tcx {
                direction: "ingress".to_string(),
                iface: NS_VETH.to_string(),
                priority: 1000,
                netns: Some(PathBuf::from(NS_PATH)),
                metadata: HashMap::new(),
            },
        );
        progs.push(res);
    }

    drop(namespace_guard);

    verify_and_delete_programs(&config, &root_db, progs);
}
