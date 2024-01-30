// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfman

use std::{fs::File, process};

use anyhow::{bail, Context};
use aya::programs::{links::FdLink, uprobe::UProbeLink, ProbeKind, UProbe};
use clap::{Args, Parser, Subcommand};
use log::debug;
use nix::sched::{setns, CloneFlags};

#[derive(Debug, Parser)]
#[clap(author, version, about, long_about = None)]
struct Cli {
    #[clap(subcommand)]
    command: Commands,
}

#[derive(Debug, Subcommand)]
enum Commands {
    /// Attach a uprobe program in the given container.
    Uprobe(UprobeArgs),
    // TODO: add additional commands: Kprobe, etc.
}

#[derive(Debug, Args)]
struct UprobeArgs {
    /// Required: path to pinned entry for bpf program on a bpffs.
    #[clap(short, long, verbatim_doc_comment)]
    program_pin_path: String,

    /// Optional: Function to attach the uprobe to.
    #[clap(short, long)]
    fn_name: Option<String>,

    /// Required: Offset added to the address of the target function (or
    /// beginning of target if no function is identified). Offsets are supported
    /// for uretprobes, but use with caution because they can result in
    /// unintended side effects.  This should be set to zero (0) if no offset
    /// is wanted.
    #[clap(short, long, verbatim_doc_comment)]
    offset: u64,

    /// Required: Library name or the absolute path to a binary or library.
    /// Example: --target "libc".
    #[clap(short, long, verbatim_doc_comment)]
    target: String,

    /// Optional: Whether the program is a uretprobe.
    /// [default: false]
    #[clap(short, long, verbatim_doc_comment)]
    retprobe: bool,

    /// Optional: Only execute uprobe for given process identification number
    /// (PID). If PID is not provided, uprobe executes for all PIDs.
    #[clap(long, verbatim_doc_comment)]
    pid: Option<i32>,

    /// Required: Host PID of the container to attach the uprobe in.
    #[clap(short, long)]
    container_pid: i32,
}

fn main() -> anyhow::Result<()> {
    env_logger::init();

    has_cap(caps::CapSet::Effective, caps::Capability::CAP_BPF);
    has_cap(caps::CapSet::Effective, caps::Capability::CAP_SYS_ADMIN);
    has_cap(caps::CapSet::Effective, caps::Capability::CAP_SYS_CHROOT);

    let bpfman_pid = process::id();

    let cli = Cli::parse();
    match cli.command {
        Commands::Uprobe(args) => execute_uprobe_attach(args, bpfman_pid),
    }
}

fn has_cap(cset: caps::CapSet, cap: caps::Capability) {
    debug!("Has {}: {}", cap, caps::has_cap(None, cset, cap).unwrap());
}

fn execute_uprobe_attach(args: UprobeArgs, bpfman_pid: u32) -> anyhow::Result<()> {
    debug!(
        "attempting to attach uprobe in container with pid {}",
        args.container_pid
    );

    let bpfman_mnt_file = match File::open(format!("/proc/{}/ns/mnt", bpfman_pid)) {
        Ok(file) => file,
        Err(e) => {
            bail!("error opening bpfman file: {e}");
        }
    };

    // First check if the file exists at /proc, which is where it should be
    // when running natively on a linux host.
    let target_mnt_file = match File::open(format!("/proc/{}/ns/mnt", args.container_pid)) {
        Ok(file) => file,
        // If that doesn't work, check for it in /host/proc, which is where it should
        // be in a kubernetes deployment.
        Err(_) => match File::open(format!("/host/proc/{}/ns/mnt", args.container_pid)) {
            Ok(file) => file,
            Err(e) => {
                bail!("error opening target file: {e}");
            }
        },
    };

    let mut uprobe = UProbe::from_pin(args.program_pin_path.clone(), ProbeKind::UProbe)
        .context("failed to get UProbe from pin file")?;

    // Set namespace to target namespace
    set_ns(
        target_mnt_file,
        CloneFlags::CLONE_NEWNS,
        args.container_pid as u32,
    )?;

    let attach_result = uprobe.attach(args.fn_name.as_deref(), args.offset, args.target, args.pid);

    let link_id = match attach_result {
        Ok(l) => l,
        Err(e) => {
            bail!("error attaching uprobe: {e}");
        }
    };

    // Set namespace back to bpfman namespace
    set_ns(bpfman_mnt_file, CloneFlags::CLONE_NEWNS, bpfman_pid)?;

    let owned_link: UProbeLink = uprobe
        .take_link(link_id)
        .expect("take_link failed for uprobe");
    let fd_link: FdLink = owned_link
        .try_into()
        .expect("unable to get owned uprobe attach link");

    fd_link.pin(format!("{}_link", args.program_pin_path))?;

    Ok(())
}

fn set_ns(file: File, nstype: CloneFlags, pid: u32) -> anyhow::Result<()> {
    let setns_result = setns(file, nstype);
    match setns_result {
        Ok(_) => Ok(()),
        Err(e) => {
            bail!(
                "error setting ns to PID {} {:?} namespace. error: {}",
                pid,
                nstype,
                e
            );
        }
    }
}
