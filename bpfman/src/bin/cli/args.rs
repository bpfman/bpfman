// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::path::PathBuf;

use bpfman_api::ProgramType;
use clap::{Args, Parser, Subcommand};
use hex::FromHex;

#[derive(Parser, Debug)]
#[command(
    long_about = "An eBPF manager focusing on simplifying the deployment and administration of eBPF programs."
)]
#[command(name = "bpfman")]
#[command(disable_version_flag = true)]
pub(crate) struct Cli {
    #[command(subcommand)]
    pub(crate) command: Commands,
}

#[derive(Subcommand, Debug)]
pub(crate) enum Commands {
    /// Load an eBPF program on the system.
    #[command(subcommand)]
    Load(LoadSubcommand),
    /// Unload an eBPF program using the program id.
    Unload(UnloadArgs),
    /// List all eBPF programs loaded via bpfman.
    List(ListArgs),
    /// Get an eBPF program using the program id.
    Get(GetArgs),
    /// eBPF Bytecode Image related commands.
    #[command(subcommand)]
    Image(ImageSubCommand),
    /// Run bpfman as a service.
    #[command(subcommand)]
    System(SystemSubcommand),
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum LoadSubcommand {
    /// Load an eBPF program from a local .o file.
    File(LoadFileArgs),
    /// Load an eBPF program packaged in a OCI container image from a given registry.
    Image(LoadImageArgs),
}

#[derive(Args, Debug)]
pub(crate) struct LoadFileArgs {
    /// Required: Location of local bytecode file
    /// Example: --path /run/bpfman/examples/go-xdp-counter/bpf_bpfel.o
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) path: String,

    /// Required: The name of the function that is the entry point for the BPF program.
    #[clap(short, long)]
    pub(crate) name: String,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    pub(crate) global: Option<Vec<GlobalArg>>,

    /// Optional: Specify Key/Value metadata to be attached to a program when it
    /// is loaded by bpfman.
    /// Format: <KEY>=<VALUE>
    ///
    /// This can later be used to `list` a certain subset of programs which contain
    /// the specified metadata.
    /// Example: --metadata owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata: Option<Vec<(String, String)>>,

    /// Optional: Program id of loaded eBPF program this eBPF program will share a map with.
    /// Only used when multiple eBPF programs need to share a map.
    /// Example: --map-owner-id 63178
    #[clap(long, verbatim_doc_comment)]
    pub(crate) map_owner_id: Option<u32>,

    #[clap(subcommand)]
    pub(crate) command: LoadCommands,
}

#[derive(Args, Debug)]
pub(crate) struct LoadImageArgs {
    /// Specify how the bytecode image should be pulled.
    #[command(flatten)]
    pub(crate) pull_args: PullBytecodeArgs,

    /// Optional: The name of the function that is the entry point for the BPF program.
    /// If not provided, the program name defined as part of the bytecode image will be used.
    #[clap(short, long, verbatim_doc_comment, default_value = "")]
    pub(crate) name: String,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    pub(crate) global: Option<Vec<GlobalArg>>,

    /// Optional: Specify Key/Value metadata to be attached to a program when it
    /// is loaded by bpfman.
    /// Format: <KEY>=<VALUE>
    ///
    /// This can later be used to list a certain subset of programs which contain
    /// the specified metadata.
    /// Example: --metadata owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata: Option<Vec<(String, String)>>,

    /// Optional: Program id of loaded eBPF program this eBPF program will share a map with.
    /// Only used when multiple eBPF programs need to share a map.
    /// Example: --map-owner-id 63178
    #[clap(long, verbatim_doc_comment)]
    pub(crate) map_owner_id: Option<u32>,

    #[clap(subcommand)]
    pub(crate) command: LoadCommands,
}

#[derive(Clone, Debug)]
pub(crate) struct GlobalArg {
    pub(crate) name: String,
    pub(crate) value: Vec<u8>,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum LoadCommands {
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on the XDP hook point for a given interface.
    Xdp {
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on "pass" --proceed-on "drop"
        ///
        /// [possible values: aborted, drop, pass, tx, redirect, dispatcher_return]
        ///
        /// [default: pass, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on the TC hook point for a given interface.
    Tc {
        /// Required: Direction to apply program.
        ///
        /// [possible values: ingress, egress]
        #[clap(short, long, verbatim_doc_comment)]
        direction: String,

        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on "ok" --proceed-on "pipe"
        ///
        /// [possible values: unspec, ok, reclassify, shot, pipe, stolen, queued,
        ///                   repeat, redirect, trap, dispatcher_return]
        ///
        /// [default: ok, pipe, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on a Tracepoint.
    Tracepoint {
        /// Required: The tracepoint to attach to.
        /// Example: --tracepoint "sched/sched_switch"
        #[clap(short, long, verbatim_doc_comment)]
        tracepoint: String,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF kprobe or kretprobe
    Kprobe {
        /// Required: Function to attach the kprobe to.
        #[clap(short, long)]
        fn_name: String,

        /// Optional: Offset added to the address of the function for kprobe.
        /// Not allowed for kretprobes.
        #[clap(short, long, verbatim_doc_comment)]
        offset: Option<u64>,

        /// Optional: Whether the program is a kretprobe.
        ///
        /// [default: false]
        #[clap(short, long, verbatim_doc_comment)]
        retprobe: bool,

        /// Optional: Host PID of container to attach the kprobe in.
        /// (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        container_pid: Option<i32>,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF uprobe or uretprobe
    Uprobe {
        /// Optional: Function to attach the uprobe to.
        #[clap(short, long)]
        fn_name: Option<String>,

        /// Optional: Offset added to the address of the target function (or
        /// beginning of target if no function is identified). Offsets are
        /// supported for uretprobes, but use with caution because they can
        /// result in unintended side effects.
        #[clap(short, long, verbatim_doc_comment)]
        offset: Option<u64>,

        /// Required: Library name or the absolute path to a binary or library.
        /// Example: --target "libc".
        #[clap(short, long, verbatim_doc_comment)]
        target: String,

        /// Optional: Whether the program is a uretprobe.
        ///
        /// [default: false]
        #[clap(short, long, verbatim_doc_comment)]
        retprobe: bool,

        /// Optional: Only execute uprobe for given process identification number (PID).
        /// If PID is not provided, uprobe executes for all PIDs.
        #[clap(short, long, verbatim_doc_comment)]
        pid: Option<i32>,

        /// Optional: Host PID of container to attach the uprobe in.
        /// (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        container_pid: Option<i32>,
    },
    #[command(disable_version_flag = true)]
    /// Install a fentry eBPF program
    Fentry {
        /// Required: Kernel function to attach the fentry program.
        #[clap(short, long)]
        fn_name: String,
    },
    #[command(disable_version_flag = true)]
    /// Install a fexit eBPF program
    Fexit {
        /// Required: Kernel function to attach the fexit program.
        #[clap(short, long)]
        fn_name: String,
    },
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct UnloadArgs {
    /// Required: Program id to be unloaded.
    pub(crate) id: u32,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct ListArgs {
    /// Optional: List a specific program type
    /// Example: --program-type xdp
    ///
    /// [possible values: unspec, socket-filter, probe, tc, sched-act,
    ///                   tracepoint, xdp, perf-event, cgroup-skb,
    ///                   cgroup-sock, lwt-in, lwt-out, lwt-xmit, sock-ops,
    ///                   sk-skb, cgroup-device, sk-msg, raw-tracepoint,
    ///                   cgroup-sock-addr, lwt-seg6-local, lirc-mode2,
    ///                   sk-reuseport, flow-dissector, cgroup-sysctl,
    ///                   raw-tracepoint-writable, cgroup-sockopt, tracing,
    ///                   struct-ops, ext, lsm, sk-lookup, syscall]
    #[clap(short, long, verbatim_doc_comment, hide_possible_values = true)]
    pub(crate) program_type: Option<ProgramType>,

    /// Optional: List programs which contain a specific set of metadata labels
    /// that were applied when the program was loaded with `--metadata` parameter.
    /// Format: <KEY>=<VALUE>
    ///
    /// Example: --metadata-selector owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata_selector: Option<Vec<(String, String)>>,

    /// Optional: List all programs.
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) all: bool,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct GetArgs {
    /// Required: Program id to get.
    pub(crate) id: u32,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum ImageSubCommand {
    /// Pull an eBPF bytecode image from a remote registry.
    Pull(PullBytecodeArgs),
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct PullBytecodeArgs {
    /// Required: Container Image URL.
    /// Example: --image-url quay.io/bpfman-bytecode/xdp_pass:latest
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) image_url: String,

    /// Optional: Registry auth for authenticating with the specified image registry.
    /// This should be base64 encoded from the '<username>:<password>' string just like
    /// it's stored in the docker/podman host config.
    /// Example: --registry_auth "YnjrcKw63PhDcQodiU9hYxQ2"
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) registry_auth: Option<String>,

    /// Optional: Pull policy for remote images.
    ///
    /// [possible values: Always, IfNotPresent, Never]
    #[clap(short, long, verbatim_doc_comment, default_value = "IfNotPresent")]
    pub(crate) pull_policy: String,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct ServiceArgs {
    /// Optional: Enable CSI support. Only supported when run in a Kubernetes
    /// environment with bpfman-agent.
    #[clap(long, verbatim_doc_comment)]
    pub(crate) csi_support: bool,
    /// Optional: Shutdown after N seconds of inactivity. Use 0 to disable.
    #[clap(long, verbatim_doc_comment, default_value = "15")]
    pub(crate) timeout: u64,
    #[clap(long, default_value = "/run/bpfman-sock/bpfman.sock")]
    /// Optional: Configure the location of the bpfman unix socket.
    pub(crate) socket_path: PathBuf,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum SystemSubcommand {
    /// Run bpfman as a service.
    Service(ServiceArgs),
}

/// Parse a single key-value pair
pub(crate) fn parse_key_val(s: &str) -> Result<(String, String), std::io::Error> {
    let pos = s.find('=').ok_or(std::io::ErrorKind::InvalidInput)?;
    Ok((s[..pos].to_string(), s[pos + 1..].to_string()))
}

pub(crate) fn parse_global_arg(global_arg: &str) -> Result<GlobalArg, std::io::Error> {
    let mut parts = global_arg.split('=');

    let name_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;

    let value_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;
    let value = Vec::<u8>::from_hex(value_str).map_err(|_e| std::io::ErrorKind::InvalidInput)?;
    if value.is_empty() {
        return Err(std::io::ErrorKind::InvalidInput.into());
    }

    Ok(GlobalArg {
        name: name_str.to_string(),
        value,
    })
}
