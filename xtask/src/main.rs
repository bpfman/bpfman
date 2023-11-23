mod build_ebpf;
mod copy;
mod integration_test;
mod protobuf;
mod run;

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
    };

    if let Err(e) = ret {
        eprintln!("{e:#}");
        exit(1);
    }
}
