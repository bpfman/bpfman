mod build_completion;
mod build_ebpf;
mod build_manpage;
mod copy;
mod integration_test;
mod protobuf;
mod run;
mod workspace;

use std::process::exit;

use clap::Parser;

#[derive(Debug, Parser)]
#[clap(author, version, about, long_about = None)]
pub struct Options {
    #[clap(subcommand)]
    command: Command,
}

#[derive(Debug, Parser)]
enum Command {
    /// Build the eBPF bytecode for programs used in the integration tests.
    BuildEbpf(build_ebpf::Options),
    /// Build the gRPC protobuf files.
    BuildProto(protobuf::Options),
    /// Prep the system for using bpfman by copying binaries to "/usr/sbin/".
    Copy(copy::Options),
    /// Run bpfman on the local host.
    Run(run::Options),
    /// Run the integration tests for bpfman.
    IntegrationTest(integration_test::Options),
    /// Build the man pages for bpfman.
    BuildManPage(build_manpage::Options),
    /// Build the completion scripts for bpfman.
    BuildCompletion(build_completion::Options),
}

fn main() {
    let opts = Options::parse();

    use Command::*;
    let ret = match opts.command {
        BuildEbpf(opts) => build_ebpf::build_ebpf(opts),
        BuildProto(opts) => protobuf::build(opts),
        Copy(opts) => copy::copy(opts),
        Run(opts) => run::run(opts),
        IntegrationTest(opts) => integration_test::test(opts),
        BuildManPage(opts) => build_manpage::build_manpage(opts),
        BuildCompletion(opts) => build_completion::build_completion(opts),
    };

    if let Err(e) = ret {
        eprintln!("{e:#}");
        exit(1);
    }
}
