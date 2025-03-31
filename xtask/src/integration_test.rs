use std::{
    io::BufReader,
    path::PathBuf,
    process::{Child, Command, Stdio},
};

use anyhow::{Context as _, bail};
use cargo_metadata::{Artifact, CompilerMessage, Message, Target};
use clap::Parser;

#[derive(Debug, Parser)]
pub struct Options {
    /// Optional: Build and run the release target
    #[clap(long)]
    pub release: bool,
    /// Optional: The command used to wrap your application
    #[clap(short, long, default_value = "sudo -E")]
    pub runner: String,
    /// An optional list of test cases to execute. All test cases will be
    /// executed if not provided.
    /// Example: cargo xtask integration-test -- test_load_unload_tracepoint_maps test_load_unload_tc_maps
    #[clap(name = "tests", verbatim_doc_comment, last = true)]
    pub run_args: Vec<String>,
    /// Optional: The tag used for all the integration-test bytecode images.
    /// Example cargo xtask integration-test --bytecode-image-tag test-tag
    #[clap(short, long, default_value = "latest")]
    pub bytecode_image_tag: String,
}

/// Build the project
fn build(opts: &Options) -> Result<Vec<(String, PathBuf)>, anyhow::Error> {
    let mut cmd = Command::new("cargo");
    let mut args = vec!["test", "--message-format=json", "--no-run"];
    if opts.release {
        args.push("--release")
    }
    args.push("--package");
    args.push("integration-test");
    cmd.args(&args);

    let mut child = cmd
        .stdout(Stdio::piped())
        .spawn()
        .with_context(|| format!("failed to spawn {cmd:?}"))?;
    let Child { stdout, .. } = &mut child;

    let stdout = stdout.take().unwrap();
    let stdout = BufReader::new(stdout);
    let mut executables = Vec::new();
    for message in Message::parse_stream(stdout) {
        #[allow(clippy::collapsible_match)]
        match message.context("valid JSON")? {
            Message::CompilerArtifact(Artifact {
                executable,
                target: Target { name, .. },
                ..
            }) => {
                if let Some(executable) = executable {
                    executables.push((name, executable.into()));
                }
            }
            Message::CompilerMessage(CompilerMessage { message, .. }) => {
                for line in message.rendered.unwrap_or_default().split('\n') {
                    println!("cargo:warning={line}");
                }
            }
            Message::TextLine(line) => {
                println!("{line}");
            }
            _ => {}
        }
    }

    let status = child
        .wait()
        .with_context(|| format!("failed to wait for {cmd:?}"))?;
    if status.code() != Some(0) {
        bail!("{cmd:?} failed: {status:?}")
    }
    Ok(executables)
}

/// Build and run the project
pub fn test(opts: Options) -> Result<(), anyhow::Error> {
    let executables = build(&opts).context("Error while building userspace application")?;
    for (name, path) in executables.into_iter() {
        println!("Running test: {}", name);
        // arguments to pass to the application
        let mut run_args: Vec<_> = opts.run_args.iter().map(String::as_str).collect();

        // configure args
        let mut args: Vec<_> = opts.runner.trim().split_terminator(' ').collect();
        args.push(path.to_str().expect("Invalid path"));
        args.append(&mut run_args);
        args.push("--test-threads=1");

        let tag = opts.bytecode_image_tag.clone();
        let bytecode_images: Vec<(String, String)> = vec![
            (
                "XDP_PASS_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/xdp_pass:{tag}"),
            ),
            (
                "TC_PASS_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/tc_pass:{tag}"),
            ),
            (
                "TRACEPOINT_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/tracepoint:{}", tag),
            ),
            (
                "UPROBE_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/uprobe:{}", tag),
            ),
            (
                "URETPROBE_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/uretprobe:{}", tag),
            ),
            (
                "KPROBE_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/kprobe:{}", tag),
            ),
            (
                "KRETPROBE_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/kretprobe:{}", tag),
            ),
            (
                "XDP_COUNTER_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/go-xdp-counter:{}", tag),
            ),
            (
                "TC_COUNTER_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/go-tc-counter:{}", tag),
            ),
            (
                "TRACEPOINT_COUNTER_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/go-tracepoint-counter:{tag}"),
            ),
            (
                "FENTRY_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/fentry:{tag}"),
            ),
            (
                "FEXIT_IMAGE_LOC".to_string(),
                format!("quay.io/bpfman-bytecode/fexit:{tag}"),
            ),
        ];

        // spawn the command
        let mut cmd = Command::new(args.first().expect("No first argument"))
            .args(args.iter().skip(1))
            .envs(bytecode_images)
            .stdout(Stdio::inherit())
            .stdout(Stdio::inherit())
            .spawn()
            .with_context(|| "failed to spawn test".to_string())?;

        let status = cmd
            .wait()
            .with_context(|| format!("failed to wait for {cmd:?}"))?;
        if status.code() != Some(0) {
            bail!("{cmd:?} failed: {status:?}")
        }
    }
    Ok(())
}
