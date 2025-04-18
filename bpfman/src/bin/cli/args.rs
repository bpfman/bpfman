// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{
    io::ErrorKind,
    path::{Path, PathBuf},
    str::FromStr,
};

use bpfman::{
    errors::ParseError,
    types::{BpfProgType, Location, ProgramType},
};
use clap::{ArgGroup, Args, Parser, Subcommand};
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
    /// Unload an eBPF program using the Program Id.
    Unload(UnloadArgs),
    /// Attach an eBPF program to a hook point using the Program Id.
    Attach(AttachArgs),
    /// Detach an eBPF program from a hook point using the Link Id.
    Detach(DetachArgs),
    /// List all loaded eBPF programs loaded or attached links.
    #[command(subcommand)]
    List(ListSubcommand),
    /// Get a loaded eBPF program or program attachment link.
    #[command(subcommand)]
    Get(GetSubcommand),
    /// eBPF Bytecode Image related commands.
    #[command(subcommand)]
    Image(Box<ImageSubCommand>),
    /// Generate man pages for bpfman.
    #[clap(hide = true)]
    Man(crate::manpage::Args),
    /// Generate shell completions for bpfman.
    #[clap(hide = true)]
    Completions(crate::completions::Args),
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
    /// Required: The program type and eBPF function name that is the entry point
    /// for the eBPF program.
    /// Format <TYPE>:<FUNC_NAME>
    ///
    /// For fentry and fexit, the function that is being attached to is also
    /// required at load time, so the format for fentry and fexit includes attach
    /// function.
    /// Format <TYPE>:<FUNC_NAME>:<ATTACH_FUNC>
    ///
    /// If the bytecode file contains multiple eBPF programs that need to be
    /// loaded, multiple eBPF programs can be entered by separating each
    /// <TYPE>:<FUNC_NAME> pair with a space.
    /// Example: --programs xdp:xdp_stats kprobe:kprobe_counter
    /// Example: --programs fentry:test_fentry:do_unlinkat
    ///
    /// [possible values for <TYPE>: fentry, fexit, kprobe, tc, tcx, tracepoint,
    ///                              uprobe, xdp]
    #[clap(long, verbatim_doc_comment, num_args(1..), value_parser=parse_program_type, required = true)]
    pub(crate) programs: Vec<(String, Vec<String>)>,
    // TODO(frobware) - this could become more strongly-typed vis-a-vis stringly-typed.
    // #[clap(value_parser = ProgramType::parse)]
    // programs: Vec<ProgramType>
    /// Required: Location of local bytecode file
    /// Example: --path /run/bpfman/examples/go-xdp-counter/bpf_x86_bpfel.o
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) path: String,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures. Multiple values can be enter by
    /// separating each <NAME>=<Hex Value> pair with a space.
    /// Example: -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    pub(crate) global: Option<Vec<GlobalArg>>,

    /// Optional: Application is used to group multiple programs that are loaded together
    /// under the same load command. This actually creates a special <KEY>=<VALUE> in the
    /// metadata parameter. It can be used to filer on list commands.
    /// Example: --application TestEbpfApp
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) application: Option<String>,

    /// Optional: Specify Key/Value metadata to be attached to a program when it
    /// is loaded by bpfman.
    /// Format: <KEY>=<VALUE>
    ///
    /// This can later be used to `list` a certain subset of programs which contain
    /// the specified metadata.
    /// Example: --metadata owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata: Option<Vec<(String, String)>>,

    /// Optional: Program Id of loaded eBPF program this eBPF program will share a map with.
    /// Only used when multiple eBPF programs need to share a map.
    /// Example: --map-owner-id 63178
    #[clap(long, verbatim_doc_comment)]
    pub(crate) map_owner_id: Option<u32>,
}

#[derive(Args, Debug)]
pub(crate) struct LoadImageArgs {
    /// Required: The program type and eBPF function name that is the entry point
    /// for the eBPF program.
    /// Format <TYPE>:<FUNC_NAME>
    ///
    /// For fentry and fexit, the function that is being attached to is also
    /// required at load time, so the format for fentry and fexit includes attach
    /// function.
    /// Format <TYPE>:<FUNC_NAME>:<ATTACH_FUNC>
    ///
    /// If the bytecode file contains multiple eBPF programs that need to be
    /// loaded, multiple eBPF programs can be enter by separating each
    /// <TYPE>:<FUNC_NAME> pair with a space.
    /// Example: --programs xdp:xdp_stats kprobe:kprobe_counter
    /// Example: --programs fentry:test_fentry:do_unlinkat
    ///
    /// [possible values for <TYPE>: fentry, fexit, kprobe, tc, tcx, tracepoint,
    ///                              uprobe, xdp]
    #[clap(long, verbatim_doc_comment, num_args(1..), value_parser=parse_program_type, required = true)]
    pub(crate) programs: Vec<(String, Vec<String>)>,
    // TODO(frobware) - this could become more strongly-typed vis-a-vis stringly-typed.
    // #[clap(value_parser = ProgramType::parse)]
    // programs: Vec<ProgramType>
    /// Specify how the bytecode image should be pulled.
    #[command(flatten)]
    pub(crate) pull_args: PullBytecodeArgs,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures. Multiple values can be enter by
    /// separating each <NAME>=<Hex Value> pair with a space.
    /// Example: -g GLOBAL_u8=01 GLOBAL_u32=0A0B0C0D
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    pub(crate) global: Option<Vec<GlobalArg>>,

    /// Optional: Application is used to group multiple programs that are loaded together
    /// under the same load command. This actually creates a special <KEY>=<VALUE> in the
    /// metadata parameter. It can be used to filer on list commands.
    /// Example: --application TestEbpfApp
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) application: Option<String>,

    /// Optional: Specify Key/Value metadata to be attached to a program when it
    /// is loaded by bpfman.
    /// Format: <KEY>=<VALUE>
    ///
    /// This can later be used to list a certain subset of programs which contain
    /// the specified metadata.
    /// Example: --metadata owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata: Option<Vec<(String, String)>>,

    /// Optional: Program Id of loaded eBPF program this eBPF program will share a map with.
    /// Only used when multiple eBPF programs need to share a map.
    /// Example: --map-owner-id 63178
    #[clap(long, verbatim_doc_comment)]
    pub(crate) map_owner_id: Option<u32>,
}

#[derive(Clone, Debug)]
pub(crate) struct GlobalArg {
    pub(crate) name: String,
    pub(crate) value: Vec<u8>,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum AttachCommands {
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on the XDP hook point for a given interface.
    Xdp {
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        /// [possible values: 1-1000]
        #[clap(short, long, verbatim_doc_comment)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on pass --proceed-on drop
        ///
        /// [possible values: aborted, drop, pass, tx, redirect, dispatcher_return]
        ///
        /// [default: pass, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,

        /// Optional: The file path of the target network namespace.
        /// Example: -n /var/run/netns/bpfman-test
        #[clap(short, long, verbatim_doc_comment)]
        netns: Option<PathBuf>,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
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
        /// [possible values: 1-1000]
        #[clap(short, long, verbatim_doc_comment)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Example: --proceed-on ok --proceed-on pipe
        ///
        /// [possible values: unspec, ok, reclassify, shot, pipe, stolen, queued,
        ///                   repeat, redirect, trap, dispatcher_return]
        ///
        /// [default: ok, pipe, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,

        /// Optional: The file path of the target network namespace.
        /// Example: -n /var/run/netns/bpfman-test
        #[clap(short, long, verbatim_doc_comment)]
        netns: Option<PathBuf>,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on the TCX hook point for a given interface and
    /// direction.
    Tcx {
        /// Required: Direction to apply program.
        ///
        /// [possible values: ingress, egress]
        #[clap(short, long, verbatim_doc_comment)]
        direction: String,

        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        /// [possible values: 1-1000]
        #[clap(short, long, verbatim_doc_comment)]
        priority: i32,

        /// Optional: The file path of the target network namespace.
        /// Example: -n /var/run/netns/bpfman-test
        #[clap(short, long, verbatim_doc_comment)]
        netns: Option<PathBuf>,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install an eBPF program on a Tracepoint.
    Tracepoint {
        /// Required: The tracepoint to attach to.
        /// Example: --tracepoint "sched/sched_switch"
        #[clap(short, long, verbatim_doc_comment)]
        tracepoint: String,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install a kprobe or kretprobe eBPF probe
    Kprobe {
        /// Required: Function to attach the kprobe to.
        #[clap(short, long)]
        fn_name: String,

        /// Optional: Offset added to the address of the function for kprobe.
        /// Not allowed for kretprobes.
        #[clap(short, long, verbatim_doc_comment)]
        offset: Option<u64>,

        /// Optional: Host PID of container to attach the kprobe in.
        /// (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        container_pid: Option<i32>,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install a uprobe or uretprobe eBPF probe
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

        /// Optional: Only execute uprobe for given process identification number (PID).
        /// If PID is not provided, uprobe executes for all PIDs.
        #[clap(short, long, verbatim_doc_comment)]
        pid: Option<i32>,

        /// Optional: Host PID of container to attach the uprobe in.
        /// (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        container_pid: Option<i32>,

        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install a fentry eBPF probe
    Fentry {
        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
    #[command(disable_version_flag = true)]
    /// Install a fexit eBPF probe
    Fexit {
        /// Optional: Specify Key/Value metadata to be attached to a link when it
        /// is loaded by bpfman.
        /// Format: <KEY>=<VALUE>
        ///
        /// This can later be used to list a certain subset of links which contain
        /// the specified metadata.
        /// Example: --metadata owner=acme
        #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
        metadata: Option<Vec<(String, String)>>,
    },
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct UnloadArgs {
    /// Required: Program Id to be unloaded.
    pub(crate) program_id: u32,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct AttachArgs {
    /// Required: Program Id to be attached.
    pub(crate) program_id: u32,

    #[clap(subcommand)]
    pub(crate) command: AttachCommands,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct DetachArgs {
    /// Required: Link Id to be detached.
    pub(crate) link_id: u32,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum ListSubcommand {
    /// List all loaded eBPF programs.
    Programs(ListProgramArgs),
    /// List all loaded eBPF programs.
    #[clap(hide = true)]
    Program(ListProgramArgs),
    /// list all program attachments.
    Links(ListLinkArgs),
    /// list all program attachments.
    #[clap(hide = true)]
    Link(ListLinkArgs),
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct ListProgramArgs {
    /// Optional: List programs by a specific program type.
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
    pub(crate) program_type: Option<BpfProgType>,

    /// Optional: List programs loaded under the same application tag.
    /// Example: --application go-app
    #[clap(long, verbatim_doc_comment)]
    pub(crate) application: Option<String>,

    /// Optional: List programs which contain a specific set of metadata labels
    /// that were applied when the program was loaded with `--metadata` parameter.
    /// Format: <KEY>=<VALUE>
    /// Example: --metadata-selector owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata_selector: Option<Vec<(String, String)>>,

    /// Optional: List all programs.
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) all: bool,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct ListLinkArgs {
    /// Optional: List links attached under the same application tag.
    /// Example: --application go-app
    #[clap(long, verbatim_doc_comment)]
    pub(crate) application: Option<String>,

    /// Optional: List links with a specific program type
    /// Example: --program-type xdp
    ///
    /// [possible values: probe, tc, tracing, tracepoint, xdp
    #[clap(short, long, verbatim_doc_comment, hide_possible_values = true)]
    pub(crate) program_type: Option<BpfProgType>,

    /// Optional: List programs which contain a specific set of metadata labels
    /// that were applied when the program was loaded with `--metadata` parameter.
    /// Format: <KEY>=<VALUE>
    /// Example: --metadata-selector owner=acme
    #[clap(short, long, verbatim_doc_comment, value_parser=parse_key_val, value_delimiter = ',')]
    pub(crate) metadata_selector: Option<Vec<(String, String)>>,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
pub(crate) enum GetSubcommand {
    /// Get a loaded eBPF program using the Program Id.
    Program(GetProgramArgs),
    /// Get a loaded eBPF program's attachment using the Link Id.
    Link(GetLinkArgs),
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct GetProgramArgs {
    /// Required: Program Id to get.
    pub(crate) program_id: u32,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct GetLinkArgs {
    /// Required: Link Id to get.
    pub(crate) link_id: u32,
}

#[derive(Subcommand, Debug)]
#[command(disable_version_flag = true)]
#[allow(clippy::large_enum_variant)]
pub(crate) enum ImageSubCommand {
    /// Pull an eBPF bytecode image from a remote registry.
    Pull(PullBytecodeArgs),
    /// Build an eBPF bytecode image from local bytecode objects and push to a registry.
    ///
    /// To use, the --container-file and --tag must be included, as well as a pointer to
    /// at least one bytecode file that can be passed in several ways. Use either:
    ///
    /// * --bytecode: for a single bytecode built for the host architecture.
    ///
    /// * --cilium-ebpf-project: for a cilium/ebpf project directory which contains
    ///   multiple object files for different architectures.
    ///
    /// * --bc-386-el .. --bc-s390x-eb: to add one or more architecture specific bytecode files.
    ///
    /// Examples:
    ///    bpfman image build -f Containerfile.bytecode -t quay.io/<USER>/go-xdp-counter:test \
    ///      -b ./examples/go-xdp-counter/bpf_x86_bpfel.o
    #[clap(verbatim_doc_comment)]
    Build(BuildBytecodeArgs),
    /// Generate the OCI image labels for a given bytecode file.
    ///
    /// To use, the --container-file and --tag must be included, as well as a pointer to
    /// at least one bytecode file that can be passed in several ways. Use either:
    ///
    /// * --bytecode: for a single bytecode built for the host architecture.
    ///
    /// * --cilium-ebpf-project: for a cilium/ebpf project directory which contains
    ///   multiple object files for different architectures.
    ///
    /// * --bc-386-el .. --bc-s390x-eb: to add one or more architecture specific bytecode files.
    ///
    /// Examples:
    ///   bpfman image generate-build-args --bc-amd64-el ./examples/go-xdp-counter/bpf_x86_bpfel.o
    #[clap(verbatim_doc_comment)]
    GenerateBuildArgs(GenerateArgs),
}

/// GoArch represents the architectures understood by golang when the GOOS=linux.
/// They are used here since the OCI spec and most container tools also use them.
/// This structure is also the centralized entry point for specifying ALL multi-arch
/// eBPF bytecode building.
#[derive(Debug, Clone)]
pub(crate) enum GoArch {
    X386,
    Amd64,
    Arm,
    Arm64,
    Loong64,
    Mips,
    Mipsle,
    Mips64,
    Mips64le,
    Ppc64,
    Ppc64le,
    Riscv64,
    S390x,
}

impl FromStr for GoArch {
    type Err = std::io::Error;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "386" => Ok(GoArch::X386),
            "amd64" => Ok(GoArch::Amd64),
            "arm" => Ok(GoArch::Arm),
            "arm64" => Ok(GoArch::Arm64),
            "loong64" => Ok(GoArch::Loong64),
            "mips" => Ok(GoArch::Mips),
            "mipsle" => Ok(GoArch::Mipsle),
            "mips64" => Ok(GoArch::Mips64),
            "mips64le" => Ok(GoArch::Mips64le),
            "ppc64" => Ok(GoArch::Ppc64),
            "ppc64le" => Ok(GoArch::Ppc64le),
            "riscv64" => Ok(GoArch::Riscv64),
            "s390x" => Ok(GoArch::S390x),
            _ => Err(std::io::Error::new(
                ErrorKind::InvalidInput,
                "not a valid bytecode arch, please refer to https://go.dev/doc/install/source#environment for valid GOARCHes when GOOS=linux.",
            )),
        }
    }
}

impl GoArch {
    /// Converts GoArch to a platform string ($GOOS/$GOARCH) that the container
    /// runtimes understand.
    pub(crate) fn get_platform(&self) -> String {
        match self {
            GoArch::X386 => "linux/386".to_string(),
            GoArch::Amd64 => "linux/amd64".to_string(),
            GoArch::Arm => "linux/arm".to_string(),
            GoArch::Arm64 => "linux/arm64".to_string(),
            GoArch::Loong64 => "linux/loong64".to_string(),
            GoArch::Mips => "linux/mips".to_string(),
            GoArch::Mipsle => "linux/mipsle".to_string(),
            GoArch::Mips64 => "linux/mips64".to_string(),
            GoArch::Mips64le => "linux/mips64le".to_string(),
            GoArch::Ppc64 => "linux/ppc64".to_string(),
            GoArch::Ppc64le => "linux/ppc64le".to_string(),
            GoArch::Riscv64 => "linux/riscv64".to_string(),
            GoArch::S390x => "linux/s390x".to_string(),
        }
    }

    /// This must be in sync with the build args described in the
    /// Containerfile.bytecode.multi.arch file.
    pub(crate) fn get_build_arg(&self, bc: &Path) -> String {
        match self {
            GoArch::X386 => format!("BC_386_EL={}", bc.display()),
            GoArch::Amd64 => format!("BC_AMD64_EL={}", bc.display()),
            GoArch::Arm => format!("BC_ARM_EL={}", bc.display()),
            GoArch::Arm64 => format!("BC_ARM64_EL={}", bc.display()),
            GoArch::Loong64 => format!("BC_LOONG64_EL={}", bc.display()),
            GoArch::Mips => format!("BC_MIPS_EB={}", bc.display()),
            GoArch::Mipsle => format!("BC_MIPSLE_EL={}", bc.display()),
            GoArch::Mips64 => format!("BC_MIPS64_EB={}", bc.display()),
            GoArch::Mips64le => format!("BC_MIPS64LE_EL={}", bc.display()),
            GoArch::Ppc64 => format!("BC_PPC64_EB={}", bc.display()),
            GoArch::Ppc64le => format!("BC_PPC64LE_EL={}", bc.display()),
            GoArch::Riscv64 => format!("BC_RISCV64_EL={}", bc.display()),
            GoArch::S390x => format!("BC_S390X_EB={}", bc.display()),
        }
    }

    /// Discovers the GoArch based on the cilium/ebpf project file-naming conventions.
    pub(crate) fn from_cilium_ebpf_file_str(s: &str) -> Result<Self, std::io::Error> {
        if s.contains("x86") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Amd64)
        } else if s.contains("arm64") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Arm64)
        } else if s.contains("arm") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Arm)
        } else if s.contains("loongarch") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Loong64)
        } else if s.contains("mips") && s.contains("bpfeb") && s.contains(".o") {
            Ok(GoArch::Mips)
        } else if s.contains("powerpc") && s.contains("bpfeb") && s.contains(".o") {
            Ok(GoArch::Ppc64)
        } else if s.contains("powerpc") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Ppc64le)
        } else if s.contains("riscv") && s.contains("bpfel") && s.contains(".o") {
            Ok(GoArch::Riscv64)
        } else if s.contains("s390") && s.contains("bpfeb") && s.contains(".o") {
            Ok(GoArch::S390x)
        } else {
            Err(std::io::Error::new(
                ErrorKind::InvalidInput,
                "not a valid cilium/ebpf bytecode filename, please refer to https://github.com/cilium/ebpf/blob/main/cmd/bpf2go/gen/target.go#L14",
            ))
        }
    }
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct BuildBytecodeArgs {
    /// Required: Name and optionally a tag in the name:tag format.
    /// Example: --tag quay.io/bpfman-bytecode/xdp_pass:latest
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) tag: String,

    /// Required: Dockerfile to use for building the image.
    /// Example: --container_file Containerfile.bytecode
    #[clap(short = 'f', long, verbatim_doc_comment)]
    pub(crate) container_file: PathBuf,

    /// Optional: Container runtime to use, works with docker or podman, defaults to docker
    /// Example: --runtime podman
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) runtime: Option<String>,

    #[clap(flatten)]
    pub(crate) bytecode_file: BytecodeFile,
}

#[derive(Args, Debug)]
#[clap(group(
    ArgGroup::new("bytecodefile")
        .multiple(false)
        .conflicts_with("multi-arch")
        .args(&["bytecode", "cilium_ebpf_project"]),
))]
#[clap(group(
    ArgGroup::new("multi-arch")
        .multiple(true)
        .args(&["bc_386_el", "bc_amd64_el", "bc_arm_el", "bc_arm64_el", "bc_loong64_el", "bc_mips_eb", "bc_mipsle_el", "bc_mips64_eb", "bc_mips64le_el", "bc_ppc64_eb", "bc_ppc64le_el", "bc_riscv64_el", "bc_s390x_eb"]),
))]
#[command(disable_version_flag = true)]
#[group(required = true)]
pub(crate) struct BytecodeFile {
    /// Optional: bytecode file to use for building the image assuming host architecture.
    /// Example: -b ./examples/go-xdp-counter/bpf_x86_bpfel.o
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) bytecode: Option<PathBuf>,

    /// Optional: If specified pull multi-arch bytecode files from a cilium/ebpf formatted project
    /// where the bytecode files all contain a standard bpf_<GOARCH>_<(el/eb)>.o tag.
    /// Example: --cilium-ebpf-project ./examples/go-xdp-counter
    #[clap(short, long, verbatim_doc_comment)]
    pub(crate) cilium_ebpf_project: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming amd64 architecture.
    /// Example: --bc-386-el ./examples/go-xdp-counter/bpf_386_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_386_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming amd64 architecture.
    /// Example: --bc-amd64-el ./examples/go-xdp-counter/bpf_x86_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_amd64_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming arm architecture.
    /// Example: --bc-arm-el ./examples/go-xdp-counter/bpf_arm_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_arm_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming arm64 architecture.
    /// Example: --bc-arm64-el ./examples/go-xdp-counter/bpf_arm64_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_arm64_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming loong64 architecture.
    /// Example: --bc-loong64-el ./examples/go-xdp-counter/bpf_loong64_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_loong64_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming mips architecture.
    /// Example: --bc-mips-eb ./examples/go-xdp-counter/bpf_mips_bpfeb.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_mips_eb: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming mipsle architecture.
    /// Example: --bc-mipsle-el ./examples/go-xdp-counter/bpf_mipsle_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_mipsle_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming mips64 architecture.
    /// Example: --bc-mips64-eb ./examples/go-xdp-counter/bpf_mips64_bpfeb.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_mips64_eb: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming mips64le architecture.
    /// Example: --bc-mips64le-el ./examples/go-xdp-counter/bpf_mips64le_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_mips64le_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming ppc64 architecture.
    /// Example: --bc-ppc64-eb ./examples/go-xdp-counter/bpf_ppc64_bpfeb.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_ppc64_eb: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming ppc64le architecture.
    /// Example: --bc-ppc64le-el ./examples/go-xdp-counter/bpf_ppc64le_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_ppc64le_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming riscv64 architecture.
    /// Example: --bc-riscv64-el ./examples/go-xdp-counter/bpf_riscv64_bpfel.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_riscv64_el: Option<PathBuf>,

    /// Optional: bytecode file to use for building the image assuming s390x architecture.
    /// Example: --bc-s390x-eb ./examples/go-xdp-counter/bpf_s390x_bpfeb.o
    #[clap(long, verbatim_doc_comment, group = "multi-arch")]
    pub(crate) bc_s390x_eb: Option<PathBuf>,
}

#[derive(Args, Debug)]
#[command(disable_version_flag = true)]
pub(crate) struct GenerateArgs {
    #[clap(flatten)]
    pub(crate) bytecode: BytecodeFile,
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

pub(crate) fn parse_program_type(
    program_type: &str,
) -> Result<(String, Vec<String>), std::io::Error> {
    let mut parts = program_type.split(':').peekable();
    let type_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;
    parts.peek().ok_or(std::io::ErrorKind::InvalidInput)?;
    let rest = parts.map(|x| x.to_string()).collect::<Vec<String>>();
    Ok((type_str.to_string(), rest))
}

/// Represents either a local or image-based eBPF program load
/// request. This enum abstracts over [`LoadFileArgs`] and
/// [`LoadImageArgs`], allowing shared logic to operate generically on
/// both.
pub(crate) enum LoadArgs<'a> {
    File(&'a LoadFileArgs),
    Image(&'a LoadImageArgs),
}

impl LoadArgs<'_> {
    pub(crate) fn get_source(&self) -> anyhow::Result<Location> {
        match self {
            LoadArgs::File(args) => Ok(Location::File(args.path.clone())),
            LoadArgs::Image(args) => Ok((&args.pull_args).try_into().map(Location::Image)?),
        }
    }

    pub(crate) fn get_programs(&self) -> &[(String, Vec<String>)] {
        match self {
            LoadArgs::File(file_args) => &file_args.programs,
            LoadArgs::Image(image_args) => &image_args.programs,
        }
    }

    pub(crate) fn get_global_data(&self) -> Option<Vec<(String, Vec<u8>)>> {
        match self {
            LoadArgs::File(file_args) => file_args.global.as_deref().map(Self::to_key_value_pairs),
            LoadArgs::Image(image_args) => {
                image_args.global.as_deref().map(Self::to_key_value_pairs)
            }
        }
    }

    pub(crate) fn get_metadata(&self) -> Option<Vec<(String, String)>> {
        match self {
            LoadArgs::File(file_args) => file_args.metadata.clone(),
            LoadArgs::Image(image_args) => image_args.metadata.clone(),
        }
    }

    pub(crate) fn get_map_owner_id(&self) -> Option<u32> {
        match self {
            LoadArgs::File(file_args) => file_args.map_owner_id,
            LoadArgs::Image(image_args) => image_args.map_owner_id,
        }
    }

    pub(crate) fn to_key_value_pairs(global: &[GlobalArg]) -> Vec<(String, Vec<u8>)> {
        global
            .iter()
            .map(|arg| (arg.name.clone(), arg.value.clone()))
            .collect()
    }

    pub(crate) fn parse_program_types(&self) -> Result<Vec<ProgramType>, ParseError> {
        self.get_programs()
            .iter()
            .map(|(kind, parts)| {
                let s = if parts.is_empty() {
                    kind.clone()
                } else {
                    format!("{}:{}", kind, parts.join(":"))
                };
                ProgramType::parse(&s)
            })
            .collect()
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_parse_program_type() {
        let result = parse_program_type("xdp:foo").unwrap();
        assert_eq!(result, ("xdp".to_string(), vec!["foo".to_string()]));

        let result = parse_program_type("fentry:foo:bar").unwrap();
        assert_eq!(
            result,
            (
                "fentry".to_string(),
                vec!["foo".to_string(), "bar".to_string()]
            )
        );

        let result = parse_program_type("xdp").unwrap_err();
        assert_eq!(result.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn test_parse_global_arg() {
        let result = parse_global_arg("foo=1234").unwrap();
        assert_eq!(result.name, "foo");
        assert_eq!(result.value, vec![0x12, 0x34]);

        // This is an error case also, but perhaps it should be allowed?
        let result = parse_global_arg("foo=0x1234").unwrap_err();
        assert_eq!(result.kind(), std::io::ErrorKind::InvalidInput);

        let result = parse_global_arg("foo=bar").unwrap_err();
        assert_eq!(result.kind(), std::io::ErrorKind::InvalidInput);

        let result = parse_global_arg("foo").unwrap_err();
        assert_eq!(result.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn test_parse_key_val() {
        let result = parse_key_val("foo=bar").unwrap();
        assert_eq!(result, ("foo".to_string(), "bar".to_string()));

        let result = parse_key_val("foo").unwrap_err();
        assert_eq!(result.kind(), std::io::ErrorKind::InvalidInput);
    }
}
