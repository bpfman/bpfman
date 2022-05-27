use std::process::Command;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("cargo:rerun-if-changed=proto/bpfd.proto");
    tonic_build::compile_protos("proto/bpfd.proto")?;
    println!("cargo:rerun-if-changed=../bpfd-ebpf");
    Command::new("make")
        .current_dir("../bpfd-ebpf")
        .output()
        .expect("eBPF failed to compile");
    Ok(())
}
