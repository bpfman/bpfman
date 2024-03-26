mod build_completion;
mod build_ebpf;
mod build_manpage;
mod copy;
mod integration_test;
mod lint;
mod protobuf;
mod public_api;
mod run;
mod unit_test;
mod workspace;

use std::process::exit;

use cargo_metadata::MetadataCommand;
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
    /// Generate the public API documentation for bpfman.
    PublicApi(public_api::Options),
    /// Run lint.
    Lint(lint::Options),
    /// Run unit tests.
    UnitTest(unit_test::Options),
}

fn main() {
    let opts = Options::parse();

    let metadata = MetadataCommand::new()
        .no_deps()
        .exec()
        .expect("failed to run cargo metadata");

    use Command::*;
    let ret = match opts.command {
        BuildEbpf(opts) => build_ebpf::build_ebpf(opts),
        BuildProto(opts) => protobuf::build(opts),
        Copy(opts) => copy::copy(opts),
        Run(opts) => run::run(opts),
        IntegrationTest(opts) => integration_test::test(opts),
        BuildManPage(opts) => build_manpage::build_manpage(opts),
        BuildCompletion(opts) => build_completion::build_completion(opts),
        PublicApi(opts) => public_api::public_api(opts, metadata),
        Lint(_) => lint::lint(),
        UnitTest(opts) => unit_test::unit_test(opts),
    };

    if let Err(e) = ret {
        eprintln!("{e:#}");
        exit(1);
    }
}
