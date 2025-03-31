use std::{
    collections::HashMap,
    fs::{self, File},
    io::{Read, Write},
    path::{Path, PathBuf},
    process::Command,
    str::FromStr,
    thread::sleep,
    time::Duration,
};

use anyhow::{Result, bail};
use bpfman::{
    add_programs, attach_program,
    config::Config,
    list_programs, remove_program,
    types::{
        AttachInfo, FentryProgram, FexitProgram, KprobeProgram, ListFilter, Location, Program,
        ProgramData, TcProgram, TcxProgram, TracepointProgram, UprobeProgram, XdpProgram,
    },
};
use lazy_static::lazy_static;
use sled::Db;

pub const NS_NAME: &str = "bpfman-int-test";
pub const NS_PATH: &str = "/var/run/netns/bpfman-int-test";

const HOST_VETH: &str = "veth-bpfm-host";
pub const NS_VETH: &str = "veth-bpfm-ns";

// The default prefix can be overriden by setting the BPFMAN_IP_PREFIX environment variable
const DEFAULT_IP_PREFIX: &str = "172.37.37";
const IP_MASK: &str = "24";
const HOST_IP_ID: &str = "1";
const NS_IP_ID: &str = "2";

pub const DEFAULT_BPFMAN_IFACE: &str = HOST_VETH;

const PING_FILE_NAME: &str = "/tmp/bpfman_ping.log";
const TRACE_PIPE_FILE_NAME: &str = "/tmp/bpfman_trace_pipe.log";

lazy_static! {
    pub static ref XDP_PASS_IMAGE_LOC: String = std::env::var("XDP_PASS_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/xdp_pass:latest"));
    pub static ref TC_PASS_IMAGE_LOC: String = std::env::var("TC_PASS_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/tc_pass:latest"));
    pub static ref TCX_TEST_IMAGE_LOC: String = std::env::var("TCX_TEST_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/tcx_test:latest"));
    pub static ref TRACEPOINT_IMAGE_LOC: String = std::env::var("TRACEPOINT_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/tracepoint:latest"));
    pub static ref UPROBE_IMAGE_LOC: String = std::env::var("UPROBE_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/uprobe:latest"));
    pub static ref URETPROBE_IMAGE_LOC: String = std::env::var("URETPROBE_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/uretprobe:latest"));
    pub static ref KPROBE_IMAGE_LOC: String = std::env::var("KPROBE_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/kprobe:latest"));
    pub static ref KRETPROBE_IMAGE_LOC: String = std::env::var("KRETPROBE_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/kretprobe:latest"));
    pub static ref XDP_COUNTER_IMAGE_LOC: String = std::env::var("XDP_COUNTER_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/go-xdp-counter:latest"));
    pub static ref TC_COUNTER_IMAGE_LOC: String = std::env::var("TC_COUNTER_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/go-tc-counter:latest"));
    pub static ref TRACEPOINT_COUNTER_IMAGE_LOC: String = std::env::var(
        "TRACEPOINT_COUNTER_IMAGE_LOC"
    )
    .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/go-tracepoint-counter:latest"));
    pub static ref FENTRY_IMAGE_LOC: String = std::env::var("FENTRY_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/fentry:latest"));
    pub static ref FEXIT_IMAGE_LOC: String = std::env::var("FEXIT_IMAGE_LOC")
        .unwrap_or_else(|_| String::from("quay.io/bpfman-bytecode/fexit:latest"));
}

pub const XDP_PASS_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/xdp_pass.bpf/bpf_x86_bpfel.o";
pub const TC_PASS_FILE_LOC: &str = "tests/integration-test/bpf/.output/tc_pass.bpf/bpf_x86_bpfel.o";
pub const TCX_TEST_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/tcx_test.bpf/bpf_x86_bpfel.o";
pub const TRACEPOINT_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/tp_openat.bpf/bpf_x86_bpfel.o";
pub const UPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/uprobe.bpf/bpf_x86_bpfel.o";
pub const URETPROBE_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/uprobe.bpf/bpf_x86_bpfel.o";
pub const KPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/kprobe.bpf/bpf_x86_bpfel.o";
pub const KRETPROBE_FILE_LOC: &str =
    "tests/integration-test/bpf/.output/kprobe.bpf/bpf_x86_bpfel.o";
pub const FENTRY_FILE_LOC: &str = "tests/integration-test/bpf/.output/fentry.bpf/bpf_x86_bpfel.o";
pub const FEXIT_FILE_LOC: &str = "tests/integration-test/bpf/.output/fentry.bpf/bpf_x86_bpfel.o";

pub const XDP_PASS_NAME: &str = "pass";
pub const XDP_COUNTER_NAME: &str = "xdp_stats";
pub const TC_PASS_NAME: &str = "pass";
pub const TC_COUNTER_NAME: &str = "stats";
pub const TCX_TEST_PASS_NAME: &str = "tcx_pass";
pub const TCX_TEST_NEXT_NAME: &str = "tcx_next";
pub const TCX_TEST_DROP_NAME: &str = "tcx_drop";
pub const FENTRY_FEXIT_KERNEL_FUNCTION_NAME: &str = "do_unlinkat";
pub const TRACEPOINT_TRACEPOINT_NAME: &str = "syscalls/sys_enter_openat";
pub const TRACEPOINT_NAME: &str = "enter_openat";
pub const TRACEPOINT_COUNTER_NAME: &str = "tracepoint_kill_recorder";
pub const UPROBE_FUNCTION_NAME: &str = "trigger_bpf_program";
pub const UPROBE_TARGET: &str = "/proc/self/exe";
pub const UPROBE_NAME: &str = "my_uprobe";
// TODO: malloc works, but it would be better to have something
// that we can trigger deliberately (i.e /self/proc/exe:trigger_bpf_program)
pub const UPROBE_CONTAINER_TARGET: &str = "libc";
pub const UPROBE_CONTAINER_FUNCTION_NAME: &str = "malloc";
pub const URETPROBE_NAME: &str = "my_uretprobe";
pub const URETPROBE_FUNCTION_NAME: &str = "trigger_bpf_program";
pub const URETPROBE_TARGET: &str = "/proc/self/exe";
pub const KPROBE_NAME: &str = "my_kprobe";
pub const KPROBE_KERNEL_FUNCTION_NAME: &str = "try_to_wake_up";
pub const KRETPROBE_NAME: &str = "my_kretprobe";
pub const KRETPROBE_KERNEL_FUNCTION_NAME: &str = "try_to_wake_up";
pub const FENTRY_NAME: &str = "test_fentry";
pub const FEXIT_NAME: &str = "test_fexit";

pub(crate) fn init_logger() {
    let _ = env_logger::builder().is_test(true).try_init();
}

/// Exit on panic as well as the passing of a test
#[derive(Debug)]
pub struct ChildGuard {
    name: &'static str,
    child: std::process::Child,
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        println!("stopping {}", self.name);
        if let Err(e) = self.child.kill() {
            println!("Could not kill {}: {e}", self.name);
        }
        if let Err(e) = self.child.wait() {
            println!("Could not wait for {}: {e}", self.name);
        }
    }
}

/// Retrieve the output of tc filter show
pub fn tc_filter_list(iface: &str) -> Result<String> {
    let output = Command::new("tc")
        .args(["filter", "show", "dev", iface, "ingress"])
        .output()
        .expect("tc filter show dev lo ingress");
    let stdout = String::from_utf8(output.stdout);
    Ok(stdout.unwrap())
}

/// Verify the specified interface exists
/// TODO: make OS agnostic (network-interface crate https://lib.rs/crates/network-interface?)
pub fn iface_exists(bpfman_iface: &str) -> bool {
    let output = Command::new("ip")
        .args(["link", "show"])
        .output()
        .expect("ip link show");
    let link_out = String::from_utf8(output.stdout).unwrap();

    if link_out.contains(bpfman_iface) {
        return true;
    }

    false
}

pub struct NamespaceGuard {
    name: &'static str,
}

/// Delete namespace.  This causes the associated veth's and ip's to also get
/// deleted
fn delete_namespace(name: &'static str) {
    let status = Command::new("ip")
        .args(["netns", "delete", name])
        .status()
        .expect("could not delete namespace");

    if !status.success() {
        println!("could not delete namespace {name}: {status}");
    } else {
        println!("namespace {} deleted", name);
    }
}

impl Drop for NamespaceGuard {
    fn drop(&mut self) {
        delete_namespace(self.name);
    }
}

fn get_ip_prefix() -> String {
    match option_env!("BPFMAN_IP_PREFIX") {
        Some(ip_prefix) => ip_prefix.to_string(),
        None => DEFAULT_IP_PREFIX.to_string(),
    }
}

fn get_ip_addr(id: &str) -> String {
    format!("{}.{}", get_ip_prefix(), id)
}

fn ip_prefix_exists(prefix: &String) -> bool {
    // It sometimes takes the previous delete_namespace(NS_NAME) a little time to clean
    // everything up, so give it a little time before checking.
    sleep(Duration::from_millis(100));

    let output = Command::new("ip")
        .args(["address", "list"])
        .output()
        .expect("Failed to create namespace");

    if !output.status.success() {
        panic!("could not execute \"ip address list\" command");
    };

    let stdout = String::from_utf8(output.stdout).unwrap();
    stdout.contains(prefix)
}

/// Create a namespace [`NS_NAME`] with a veth pair and IP addresses
pub fn create_namespace() -> Result<NamespaceGuard> {
    if ip_prefix_exists(&get_ip_prefix()) {
        panic!(
            "ip prefix {} is in use, specify an available prefix with env BPFMAN_IP_PREFIX.",
            get_ip_prefix()
        );
    }

    let status = Command::new("ip")
        .args(["netns", "add", NS_NAME])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        panic!("failed to create namespace {NS_NAME}: {status}");
    }

    let status = Command::new("ip")
        .args([
            "link", "add", HOST_VETH, "type", "veth", "peer", "name", NS_VETH,
        ])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!("failed to create veth pair {HOST_VETH}-{NS_VETH}: {status}");
    }

    let status = Command::new("ip")
        .args(["link", "set", NS_VETH, "netns", NS_NAME])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!("failed to add veth {NS_VETH} to {NS_NAME}: {status}");
    }

    let ns_ip_mask = format!("{}/{}", get_ip_addr(NS_IP_ID), IP_MASK);

    let status = Command::new("ip")
        .args([
            "netns",
            "exec",
            NS_NAME,
            "ip",
            "addr",
            "add",
            &ns_ip_mask,
            "dev",
            NS_VETH,
        ])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!(
            "failed to add ip address {ns_ip_mask} to {NS_VETH}: {status}\n
        if {ns_ip_mask} is not available, specify a usable prefix with env BPFMAN_IT_PREFIX.\n
        for example: export BPFMAN_IT_PREFIX=\"192.168.1\""
        );
    }

    let host_ip_mask = format!("{}/{}", get_ip_addr(HOST_IP_ID), IP_MASK);

    let status = Command::new("ip")
        .args(["addr", "add", &host_ip_mask, "dev", HOST_VETH])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!("failed to add ip address {ns_ip_mask} to {HOST_VETH}: {status}");
    }

    let status = Command::new("ip")
        .args([
            "netns", "exec", NS_NAME, "ip", "link", "set", "dev", NS_VETH, "up",
        ])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!("failed to set dev {NS_VETH} to up: {status}");
    }

    let status = Command::new("ip")
        .args(["link", "set", "dev", HOST_VETH, "up"])
        .status()
        .expect("Failed to create namespace");

    if !status.success() {
        delete_namespace(NS_NAME);
        panic!("failed to set dev {HOST_VETH} to up: {status}");
    }

    println!("Successfully created namespace {NS_NAME}");

    Ok(NamespaceGuard { name: NS_NAME })
}

/// start a ping to the network namespace IP address with output logged to [`PING_FILE_NAME`]
pub fn start_ping() -> Result<ChildGuard> {
    let f = File::create(PING_FILE_NAME).unwrap();
    let ping_process = Command::new("ping")
        .args([get_ip_addr(NS_IP_ID)])
        .stdout(std::process::Stdio::from(f))
        .spawn()
        .map(|c| ChildGuard {
            name: "ping",
            child: c,
        })
        .expect("Failed to start ping");

    println!(
        "sucessfully started ping to namespace {} at address {}",
        NS_NAME,
        get_ip_addr(NS_IP_ID),
    );

    Ok(ping_process)
}

/// Get the ping log from [`PING_FILE_NAME`]
pub fn read_ping_log() -> Result<String> {
    let mut f = File::open(PING_FILE_NAME)?;
    let mut buffer = String::new();
    f.read_to_string(&mut buffer)?;
    Ok(buffer)
}

/// start sending /sys/kernel/debug/tracing/trace_pipe to [`TRACE_PIPE_FILE_NAME`]
pub fn start_trace_pipe() -> Result<ChildGuard> {
    // The trace_pipe is clear on read, so we start a process to read it to
    // clear any logs left over from the last test.  Kill that process and then
    // start the real one.

    // Start it
    let f = File::create(TRACE_PIPE_FILE_NAME).unwrap();
    let mut trace_process = Command::new("cat")
        .args(["/sys/kernel/debug/tracing/trace_pipe"])
        .stdout(std::process::Stdio::from(f))
        .spawn()
        .expect("Failed to start trace_pipe");

    sleep(Duration::from_secs(1));

    // Kill it
    if let Err(e) = trace_process.kill() {
        println!("Could not kill trace_pipe: {e}");
    }
    if let Err(e) = trace_process.wait() {
        println!("Could not wait for trace_pipe: {e}");
    }

    // Start it again
    let f = File::create(TRACE_PIPE_FILE_NAME).unwrap();
    let trace_process = Command::new("cat")
        .args(["/sys/kernel/debug/tracing/trace_pipe"])
        .stdout(std::process::Stdio::from(f))
        .spawn()
        .map(|c| ChildGuard {
            name: "trace_pipe",
            child: c,
        })
        .expect("Failed to start trace_pipe");

    println!("sucessfully started cat trace_pipe",);
    Ok(trace_process)
}

/// get the trace_pipe output from [`TRACE_PIPE_FILE_NAME`]
pub fn read_trace_pipe_log() -> Result<String> {
    let mut f = File::open(TRACE_PIPE_FILE_NAME)?;
    let mut buffer = String::new();
    f.read_to_string(&mut buffer)?;
    println!("trace_pipe output read to string");
    Ok(buffer)
}

/// Verify that the programs in the loaded_ids list have been loaded.  Then delete them
/// and verify that they have been deleted.
#[track_caller]
pub fn verify_and_delete_programs(config: &Config, root_db: &Db, programs: Vec<Program>) {
    // List programs
    let res = list_programs(root_db, ListFilter::default())
        .unwrap()
        .into_iter()
        .map(|p| p.get_data().get_id().unwrap())
        .collect::<Vec<_>>();

    // Verify that the programs are loaded
    for program in programs.iter().map(|p| p.get_data().get_id().unwrap()) {
        assert!(res.contains(&program));
    }

    let mut deleted_ids = vec![];
    // Delete the installed programs
    println!("Deleting bpfman program(s)");
    for program in programs.iter() {
        let id = program.get_data().get_id().unwrap();
        deleted_ids.push(id);
        remove_program(config, root_db, id).unwrap();
    }

    let check_not_loaded = || -> anyhow::Result<()> {
        // Verify bpfman list does not contain the loaded_ids of the deleted programs
        // and that there are no panics if bpfman does not contain any programs.
        let res = list_programs(root_db, ListFilter::default())
            .unwrap()
            .into_iter()
            .map(|p| p.get_data().get_id().unwrap())
            .collect::<Vec<_>>();

        // Verify that the programs are not loaded
        for id in res {
            if !deleted_ids.contains(&id) {
                bail!("Program {} is still loaded", id);
            }
        }
        Ok(())
    };

    let mut retries = 5;
    while retries > 0 {
        if check_not_loaded().is_err() && retries > 0 {
            retries -= 1;
            sleep(Duration::from_secs(1));
        } else {
            panic!("Programs are still loaded after deletion");
        }
    }
}

/// Returns true if the bpffs has entries and false if it doesn't
pub fn bpffs_has_entries(path: &str) -> bool {
    PathBuf::from(path).read_dir().unwrap().next().is_some()
}

macro_rules! add_program_inner {
    ($prog_inner_ty:expr, $prog_ty:expr, $config:ident, $root_db:ident, $name:ident, $location:ident, $globals:ident, $metadata:ident, $map_owner:ident, $attach_info:ident) => {{
        let data = ProgramData::new($location, $name, $metadata, $globals, $map_owner).unwrap();
        let prog = $prog_inner_ty(data).unwrap();
        let res = add_programs($config, $root_db, vec![$prog_ty(prog)]).unwrap();

        let installed_prog = res.into_iter().next().unwrap();

        let id = installed_prog
            .get_data()
            .get_id()
            .expect("Failed to get program id");

        let _ = attach_program($config, $root_db, id, $attach_info).unwrap();
        installed_prog
    }};
    ($prog_inner_ty:expr, $prog_ty:expr, $config:ident, $root_db:ident, $name:ident, $fn_name:ident, $location:ident, $globals:ident, $metadata:ident, $map_owner:ident, $attach_info:ident) => {{
        let data = ProgramData::new($location, $name, $metadata, $globals, $map_owner).unwrap();
        let prog = $prog_inner_ty(data, $fn_name).unwrap();
        let res = add_programs($config, $root_db, vec![$prog_ty(prog)]).unwrap();

        let installed_prog = res.into_iter().next().unwrap();

        let id = installed_prog
            .get_data()
            .get_id()
            .expect("Failed to get program id");

        let _ = attach_program($config, $root_db, id, $attach_info).unwrap();
        installed_prog
    }};
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_xdp(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        XdpProgram::new,
        Program::Xdp,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_tc(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        TcProgram::new,
        Program::Tc,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_tcx(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        TcxProgram::new,
        Program::Tcx,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_tracepoint(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        TracepointProgram::new,
        Program::Tracepoint,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_kprobe(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        KprobeProgram::new,
        Program::Kprobe,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_uprobe(
    config: &Config,
    root_db: &Db,
    name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        UprobeProgram::new,
        Program::Uprobe,
        config,
        root_db,
        name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_fentry(
    config: &Config,
    root_db: &Db,
    name: String,
    fn_name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        FentryProgram::new,
        Program::Fentry,
        config,
        root_db,
        name,
        fn_name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[track_caller]
#[allow(clippy::too_many_arguments)]
pub(crate) fn add_fexit(
    config: &Config,
    root_db: &Db,
    name: String,
    fn_name: String,
    location: Location,
    globals: HashMap<String, Vec<u8>>,
    metadata: HashMap<String, String>,
    map_owner: Option<u32>,
    attach_info: AttachInfo,
) -> Program {
    add_program_inner!(
        FexitProgram::new,
        Program::Fexit,
        config,
        root_db,
        name,
        fn_name,
        location,
        globals,
        metadata,
        map_owner,
        attach_info
    )
}

#[derive(Debug)]
pub struct DockerContainer {
    container_pid: i32,
    container_id: String,
}

impl Drop for DockerContainer {
    fn drop(&mut self) {
        let output = Command::new("docker")
            .args(["rm", "-f", self.container_id.as_str()])
            .output()
            .expect("failed to start docker");

        if output.status.success() {
            println!("Docker container {} removed", self.container_id);
        } else {
            println!("Error removing container {}", self.container_id);
        }
    }
}

impl DockerContainer {
    /// Return the container PID
    pub fn container_pid(&self) -> i32 {
        self.container_pid
    }

    /// Runs bash in the container
    pub fn bash(&self, cmd: &[u8]) {
        let mut child = Command::new("docker")
            .stdin(std::process::Stdio::piped())
            .args(["exec", &self.container_id, "/bin/bash"])
            .spawn()
            .expect("failed run ls in container");
        let input = [cmd, b"\nexit\n"].concat();
        let child_stdin = child.stdin.as_mut().expect("Failed to open stdin");
        child_stdin
            .write_all(&input)
            .expect("Failed to write to stdin");
        child.wait().expect("Failed to kill child");
    }
}

/// Starts a docker container from the nginx image
pub fn start_container() -> Result<DockerContainer> {
    let status = Command::new("systemctl")
        .args(["start", "docker"])
        .status()
        .expect("failed to start docker");
    assert!(status.success());

    let output = Command::new("docker")
        .args(["run", "--name", "mynginx1", "-p", "80:80", "-d", "nginx"])
        .output()
        .expect("failed to start nginx");

    let mut container_id = String::from_utf8(output.stdout).unwrap();
    // Get rid of trailing '\n'
    container_id.pop();

    assert!(!container_id.is_empty());

    let output = Command::new("lsns")
        .args(["-t", "pid"])
        .output()
        .expect("systemctl start docker");

    let output = String::from_utf8(output.stdout).unwrap();

    let mut container_pid: i32 = 0;
    for line in output.lines() {
        if line.contains("nginx") {
            let pid_str: Vec<&str> = line.split_whitespace().collect();
            container_pid = FromStr::from_str(pid_str[3]).unwrap();
            break;
        }
    }
    assert!(container_pid != 0);

    println!(
        "Docker container with ID {} and PID {} created",
        container_id, container_pid
    );

    Ok(DockerContainer {
        container_pid,
        container_id,
    })
}

pub struct DisableCosignGuard<'a> {
    path: &'a str,
}

impl Drop for DisableCosignGuard<'_> {
    fn drop(&mut self) {
        if Path::new(self.path).exists() {
            fs::remove_file(self.path).expect("Failed to delete file");
        }
    }
}

pub fn disable_cosign() -> Result<DisableCosignGuard<'static>> {
    let content = "[signing]\nallow_unsigned = true\nverify_enabled = false\n";
    let path = "/etc/bpfman/bpfman.toml";

    let cosign_guard = DisableCosignGuard { path };

    // Create the directory if it doesn't exist
    fs::create_dir_all("/etc/bpfman")?;

    // Write the content to the file
    let mut file = fs::File::create(path)?;
    file.write_all(content.as_bytes())?;

    println!(
        "bpfman.toml with \"verify_enabled = false\" created at {}",
        path
    );

    Ok(cosign_guard)
}
