use std::process::Command;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::compile_protos("proto/bpfd.proto")?;
    Command::new("make").current_dir("../bpfd-ebpf").output().expect("eBPF failed to compile");
    Ok(())
}
