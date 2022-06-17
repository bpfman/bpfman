use std::process::Command;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("cargo:rerun-if-changed=proto/bpfd.proto");
    tonic_build::compile_protos("proto/bpfd.proto")?;
    println!("cargo:rerun-if-changed=bpf");
    Command::new("make")
        .current_dir("./bpf")
        .output()
        .expect("eBPF failed to compile");
    Ok(())
}
