use std::{
    fs::File, io::Read, path::PathBuf, process::Command, str::FromStr, thread::sleep,
    time::Duration,
};

use anyhow::Result;
use assert_cmd::prelude::*;
use log::debug;
use predicates::str::is_empty;
use regex::Regex;

const NS_NAME: &str = "bpfman-int-test";

const HOST_VETH: &str = "veth-bpfm-host";
const NS_VETH: &str = "veth-bpfm-ns";

// The default prefix can be overriden by setting the BPFMAN_IP_PREFIX environment variable
const DEFAULT_IP_PREFIX: &str = "172.37.37";
const IP_MASK: &str = "24";
const HOST_IP_ID: &str = "1";
const NS_IP_ID: &str = "2";

pub const DEFAULT_BPFMAN_IFACE: &str = HOST_VETH;

const PING_FILE_NAME: &str = "/tmp/bpfman_ping.log";
const TRACE_PIPE_FILE_NAME: &str = "/tmp/bpfman_trace_pipe.log";

#[derive(Debug)]
pub enum LoadType {
    Image,
    File,
}

pub static LOAD_TYPES: &[LoadType] = &[LoadType::Image, LoadType::File];

pub const XDP_PASS_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/xdp_pass:latest";
pub const TC_PASS_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/tc_pass:latest";
pub const TRACEPOINT_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/tracepoint:latest";
pub const UPROBE_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/uprobe:latest";
pub const URETPROBE_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/uretprobe:latest";
pub const KPROBE_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/kprobe:latest";
pub const KRETPROBE_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/kretprobe:latest";
pub const XDP_COUNTER_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/go-xdp-counter";
pub const TC_COUNTER_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/go-tc-counter";
pub const TRACEPOINT_COUNTER_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/go-tracepoint-counter";
pub const FENTRY_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/fentry:latest";
pub const FEXIT_IMAGE_LOC: &str = "quay.io/bpfman-bytecode/fexit:latest";

pub const XDP_PASS_FILE_LOC: &str = "tests/integration-test/bpf/.output/xdp_pass.bpf.o";
pub const TC_PASS_FILE_LOC: &str = "tests/integration-test/bpf/.output/tc_pass.bpf.o";
pub const TRACEPOINT_FILE_LOC: &str = "tests/integration-test/bpf/.output/tp_openat.bpf.o";
pub const UPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/uprobe.bpf.o";
pub const URETPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/uprobe.bpf.o";
pub const KPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/kprobe.bpf.o";
pub const KRETPROBE_FILE_LOC: &str = "tests/integration-test/bpf/.output/kprobe.bpf.o";
pub const FENTRY_FILE_LOC: &str = "tests/integration-test/bpf/.output/fentry.bpf.o";
pub const FEXIT_FILE_LOC: &str = "tests/integration-test/bpf/.output/fentry.bpf.o";

/// Exit on panic as well as the passing of a test
#[derive(Debug)]
pub struct ChildGuard {
    name: &'static str,
    child: std::process::Child,
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        debug!("stopping {}", self.name);
        if let Err(e) = self.child.kill() {
            println!("Could not kill {}: {e}", self.name);
        }
        if let Err(e) = self.child.wait() {
            println!("Could not wait for {}: {e}", self.name);
        }
    }
}

/// Spawn a bpfman process
// pub fn start_bpfman() -> Result<ChildGuard> {
//     debug!("Starting bpfman");

//     let bpfman_process = Command::cargo_bin("bpfman")?
//         .args(["system", "service", "--timeout=0"])
//         .env("RUST_LOG", "bpfman=debug")
//         .spawn()
//         .map(|c| ChildGuard {
//             name: "bpfman",
//             child: c,
//         })?;

//     debug!("started process");
//     // Wait for up to 5 seconds for bpfman to be ready
//     sleep(Duration::from_millis(100));
//     for i in 1..51 {
//         if let Err(e) = Command::cargo_bin("bpfman")?.args(["list"]).ok() {
//             if i == 50 {
//                 panic!("bpfman not ready after {} ms. Error:\n{}", i * 100, e);
//             } else {
//                 sleep(Duration::from_millis(100));
//             }
//         } else {
//             break;
//         }
//     }
//     debug!("Successfully Started bpfman");

//     Ok(bpfman_process)
// }

/// Install an xdp program with bpfman
#[allow(clippy::too_many_arguments)]
pub fn add_xdp(
    iface: &str,
    priority: u32,
    globals: Option<Vec<&str>>,
    proceed_on: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
    metadata: Option<Vec<&str>>,
    map_owner_id: Option<u32>,
) -> (Result<String>, Result<String>) {
    let p = priority.to_string();
    let owner_id: String;

    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    if let Some(g) = metadata {
        args.push("--metadata");
        args.extend(g);
    }

    if let Some(owner) = map_owner_id {
        owner_id = owner.to_string();
        args.extend(["--map-owner-id", owner_id.as_str()]);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "pass", "--path", file_path]),
    }

    args.extend(["xdp", "--iface", iface, "--priority", p.as_str()]);

    if let Some(p_o) = proceed_on {
        args.push("--proceed-on");
        args.extend(p_o);
    }

    let output = Command::cargo_bin("bpfman")
        .expect("bpfman missing")
        .args(args)
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added xdp program: {:?} from: {:?}",
        prog_id, load_type
    );

    (Ok(prog_id), Ok(stdout))
}

/// Install a tc program with bpfman
#[allow(clippy::too_many_arguments)]
pub fn add_tc(
    direction: &str,
    iface: &str,
    priority: u32,
    globals: Option<Vec<&str>>,
    proceed_on: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
) -> (Result<String>, Result<String>) {
    let p = priority.to_string();

    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "pass", "--path", file_path]),
    }

    args.extend([
        "tc",
        "--direction",
        direction,
        "--iface",
        iface,
        "--priority",
        p.as_str(),
    ]);

    if let Some(p_o) = proceed_on {
        args.push("--proceed-on");
        args.extend(p_o);
    }

    let output = Command::cargo_bin("bpfman")
        .expect("bpfman missing")
        .args(args)
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added tc {} program: {:?} from: {:?}",
        direction, prog_id, load_type
    );

    (Ok(prog_id), Ok(stdout))
}

/// Install a tracepoint program with bpfman
pub fn add_tracepoint(
    globals: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
) -> (Result<String>, Result<String>) {
    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "enter_openat", "--path", file_path]),
    }

    args.extend(["tracepoint", "--tracepoint", "syscalls/sys_enter_openat"]);

    let output = Command::cargo_bin("bpfman")
        .expect("bpfman missing")
        .args(args)
        .ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added tracepoint program: {:?} from: {:?}",
        prog_id, load_type
    );
    (Ok(prog_id), Ok(stdout))
}

/// Attach a uprobe program.
///
/// If a container_pid is provided, attach it to malloc() in that namespace.
/// Otherwise, attach it to the main function in the bpfctl command.
pub fn add_uprobe(
    globals: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
    container_pid: Option<&str>,
) -> Result<String> {
    let bpfman_cmd = Command::cargo_bin("bpfman")?;
    let bpfman_path = bpfman_cmd.get_program().to_str().unwrap();

    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "my_uprobe", "--path", file_path]),
    }

    if let Some(pid) = container_pid {
        args.extend([
            "uprobe",
            "-f",
            "malloc",
            "-t",
            "libc",
            "--container-pid",
            pid,
        ]);
    } else {
        args.extend(["uprobe", "-f", "main", "-t", bpfman_path]);
    }

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added uprobe program: {:?} from: {:?}",
        prog_id, load_type
    );
    Ok(prog_id)
}

/// Attach a uretprobe program to bpfman with bpfman
pub fn add_uretprobe(
    globals: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
) -> Result<String> {
    let bpfman_cmd = Command::cargo_bin("bpfman")?;
    let bpfman_path = bpfman_cmd.get_program().to_str().unwrap();

    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "my_uretprobe", "--path", file_path]),
    }

    args.extend(["uprobe", "-f", "main", "-t", bpfman_path, "-r"]);

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added uretprobe program: {:?} from: {:?}",
        prog_id, load_type
    );
    Ok(prog_id)
}

/// Install a kprobe program with bpfman
pub fn add_kprobe(
    globals: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
) -> Result<String> {
    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "my_kprobe", "--path", file_path]),
    }

    args.extend(["kprobe", "-f", "try_to_wake_up"]);

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added kprobe program: {:?} from: {:?}",
        prog_id, load_type
    );
    Ok(prog_id)
}

/// Install a kretprobe program with bpfman
pub fn add_kretprobe(
    globals: Option<Vec<&str>>,
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
) -> Result<String> {
    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    if let Some(g) = globals {
        args.push("--global");
        args.extend(g);
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => args.extend(["-n", "my_kretprobe", "--path", file_path]),
    }

    args.extend(["kprobe", "--retprobe", "-f", "try_to_wake_up"]);

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    debug!(
        "Successfully added kretprobe program: {:?} from: {:?}",
        prog_id, load_type
    );
    Ok(prog_id)
}

/// Install a fentry or fexit program with bpfman
pub fn add_fentry_or_fexit(
    load_type: &LoadType,
    image_url: &str,
    file_path: &str,
    fentry: bool,
) -> Result<String> {
    let mut args = vec!["load"];
    match load_type {
        LoadType::Image => {
            args.push("image");
        }
        LoadType::File => {
            args.push("file");
        }
    }

    match load_type {
        LoadType::Image => args.extend(["--image-url", image_url, "--pull-policy", "Always"]),
        LoadType::File => {
            if fentry {
                args.extend(["-n", "test_fentry", "--path", file_path]);
            } else {
                args.extend(["-n", "test_fexit", "--path", file_path]);
            }
        }
    }

    if fentry {
        args.extend(["fentry", "-f", "do_unlinkat"]);
    } else {
        args.extend(["fexit", "-f", "do_unlinkat"]);
    }

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let prog_id = bpfman_output_parse_id(&stdout);
    assert!(!prog_id.is_empty());
    if fentry {
        debug!(
            "Successfully added fentry program: {:?} from: {:?}",
            prog_id, load_type
        );
    } else {
        debug!(
            "Successfully added fexit program: {:?} from: {:?}",
            prog_id, load_type
        );
    }
    Ok(prog_id)
}

/// Delete a bpfman program using bpfman
pub fn bpfman_del_program(prog_id: &str) {
    Command::cargo_bin("bpfman")
        .unwrap()
        .args(["unload", prog_id.trim()])
        .assert()
        .success()
        .stdout(is_empty());

    debug!("Successfully deleted program: \"{}\"", prog_id.trim());
}

/// Retrieve the output of bpfman list
pub fn bpfman_list(metadata_selector: Option<Vec<&str>>) -> Result<String> {
    let mut args = vec!["list"];
    if let Some(g) = metadata_selector {
        args.push("--metadata-selector");
        args.extend(g);
    }

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout);
    Ok(stdout.unwrap())
}

/// Retrieve program data for a given program with bpfman
pub fn bpfman_get(prog_id: &str) -> Result<String> {
    let output = Command::cargo_bin("bpfman")
        .unwrap()
        .args(["get", prog_id.trim()])
        .ok();

    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
    let output_prog_id = bpfman_output_parse_id(&stdout);
    assert!(!output_prog_id.is_empty());
    debug!(
        "Successfully ran \'bpfman get\' for program: {:?}",
        output_prog_id
    );
    Ok(stdout)
}

pub fn bpfman_pull_bytecode() -> Result<String> {
    let mut args = vec!["image", "pull"];

    args.extend([
        "--image-url",
        TRACEPOINT_IMAGE_LOC,
        "--pull-policy",
        "Always",
    ]);

    let output = Command::cargo_bin("bpfman")?.args(args).ok();
    let stdout = String::from_utf8(output.unwrap().stdout);
    Ok(stdout.unwrap())
}

/// Retrieve the output of bpfman list
pub fn tc_filter_list(iface: &str) -> Result<String> {
    let output = Command::new("tc")
        .args(["filter", "show", "dev", iface, "ingress"])
        .output()
        .expect("tc filter show dev lo ingress");
    let stdout = String::from_utf8(output.unwrap().stdout);
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
        debug!("namespace {} deleted", name);
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

    let stdout = String::from_utf8(output.unwrap().stdout).unwrap();
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

    debug!("Successfully created namespace {NS_NAME}");

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

    debug!(
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

    debug!("sucessfully started cat trace_pipe",);
    Ok(trace_process)
}

/// get the trace_pipe output from [`TRACE_PIPE_FILE_NAME`]
pub fn read_trace_pipe_log() -> Result<String> {
    let mut f = File::open(TRACE_PIPE_FILE_NAME)?;
    let mut buffer = String::new();
    f.read_to_string(&mut buffer)?;
    debug!("trace_pipe output read to string");
    Ok(buffer)
}

/// Verify that the programs in the loaded_ids list have been loaded.  Then delete them
/// and verify that they have been deleted.
pub fn verify_and_delete_programs(loaded_ids: Vec<String>) {
    // Verify bpfman list contains the loaded_ids of each program
    let l = bpfman_list(None).unwrap();
    for id in loaded_ids.iter() {
        assert!(l.contains(id.trim()));
    }

    // Delete the installed programs
    debug!("Deleting bpfman program(s)");
    for id in loaded_ids.iter() {
        bpfman_del_program(id)
    }

    // Verify bpfman list does not contain the loaded_ids of the deleted programs
    // and that there are no panics if bpfman does not contain any programs.
    let l = bpfman_list(None).unwrap();
    for id in loaded_ids.iter() {
        assert!(!l.contains(id.trim()));
    }
}

/// Returns true if the bpffs has entries and false if it doesn't
pub fn bpffs_has_entries(path: &str) -> bool {
    PathBuf::from(path).read_dir().unwrap().next().is_some()
}

fn bpfman_output_parse_id(stdout: &str) -> String {
    // Regex:
    //   Match the string "\n ID: ".
    //   The {2,} indicates to match the previous token (a space) between 2 and
    //   unlimited times.
    //   For the capture group (.*?), the . indicates to capture any character
    //   (except for line terminators) and the *? indicates to capture "the previous
    //   token between zero and unlimited times".
    //   The \s indicates to match any whites space.
    let re = Regex::new(r"\n ID: {2,}(.*?)\s").unwrap();
    match re.captures(stdout) {
        Some(caps) => caps[1].to_owned(),
        None => {
            debug!("\"ID:\" not found",);
            "".to_string()
        }
    }
}

pub fn bpfman_output_map_pin_path(stdout: &str) -> String {
    // Regex:
    //   Match the string "\n Maps Pin Path: ".
    //   The {2,} indicates to match the previous token (a space) between 2 and
    //   unlimited times.
    //   For the capture group (.*?), the . indicates to capture any character
    //   (except for line terminators) and the *? indicates to capture "the previous
    //   token between zero and unlimited times".
    //   The \s indicates to match any whites space.
    let re = Regex::new(r"\n Map Pin Path: {2,}(.*?)\s").unwrap();
    match re.captures(stdout) {
        Some(caps) => caps[1].to_owned(),
        None => {
            debug!("\"Map Pin Path:\" not found",);
            "".to_string()
        }
    }
}

pub fn bpfman_output_map_owner_id(stdout: &str) -> String {
    // Regex:
    //   Match the string "\n Maps Owner ID: ".
    //   The {2,} indicates to match the previous token (a space) between 2 and
    //   unlimited times.
    //   For the capture group (.*?), the . indicates to capture any character
    //   (except for line terminators) and the *? indicates to capture "the previous
    //   token between zero and unlimited times".
    //   The \s indicates to match any whites space.
    let re = Regex::new(r"\n Map Owner ID: {2,}(.*?)\s").unwrap();
    match re.captures(stdout) {
        Some(caps) => caps[1].to_owned(),
        None => {
            debug!("\"Map Pin Path:\" not found",);
            "".to_string()
        }
    }
}

pub fn bpfman_output_xdp_map_used_by(stdout: &str) -> Vec<String> {
    let mut used_by: Vec<String> = Vec::new();

    // Regex:
    //   Match the string "\n Maps Used By:".
    //   For the capture group ((.|\n)*?), the (.|\n) indicates to capture "any character
    //   (except for line terminators)" OR capture "a line-feed (newline) character".
    //   The *? indicates to capture "the previous token between zero and unlimited times"
    //   Match the string "\n Priority:".
    //
    // This is specific to XDP because other program types have different fields after
    // "Maps Used By:". Capture string will something like:
    //    "  None\n"  OR  "  1324\n"  OR "  3456    \n    3468\n"
    let re_1 = Regex::new(r"\n Maps Used By:((.|\n)*?)\n Priority:").unwrap();
    let used_by_output = match re_1.captures(stdout) {
        Some(caps) => caps[1].to_owned(),
        None => {
            debug!("\"Map Used By:\" not found",);
            return used_by;
        }
    };

    // Regex:
    //   Take the previous output, convert to a Vec of String where each
    //   is the Program Id (all digits).
    let re_2 = Regex::new(r"(\d+)").unwrap();
    for cap in re_2.captures_iter(&used_by_output) {
        used_by.push(cap[1].to_string());
    }

    used_by
}

pub fn bpfman_output_map_ids(stdout: &str) -> Vec<String> {
    let mut map_ids: Vec<String> = Vec::new();

    // Regex:
    //   Match the string "\n Map IDs:".
    //   For the capture group ((.|\n)*?), the (.|\n) indicates to capture "any character
    //   (except for line terminators)" OR capture "a line-feed (newline) character".
    //   The *? indicates to capture "the previous token between zero and unlimited times"
    //   Match the string "\n BTF ID:".
    let re_1 = Regex::new(r"\n Map IDs:((.|\n)*?)\n BTF ID:").unwrap();
    let map_ids_output = match re_1.captures(stdout) {
        Some(caps) => caps[1].to_owned(),
        None => {
            debug!("\"Map IDs:\" not found",);
            return map_ids;
        }
    };

    // Regex:
    //   Take the previous output, convert to a Vec of String where each
    //   is the Map Id (all digits).
    let re_2 = Regex::new(r"(\d+)").unwrap();
    for cap in re_2.captures_iter(&map_ids_output) {
        map_ids.push(cap[1].to_string());
    }

    map_ids
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
            debug!("Docker container {} removed", self.container_id);
        } else {
            debug!("Error removing container {}", self.container_id);
        }
    }
}

impl DockerContainer {
    /// Return the container PID
    pub fn container_pid(&self) -> i32 {
        self.container_pid
    }

    /// Runs the ls command in the container to generate some mallocs.
    pub fn ls(&self) {
        let output = Command::new("docker")
            .args(["exec", &self.container_id, "ls"])
            .output()
            .expect("failed run ls in container");
        assert!(output.status.success());
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

    debug!(
        "Docker container with ID {} and PID {} created",
        container_id, container_pid
    );

    Ok(DockerContainer {
        container_pid,
        container_id,
    })
}
