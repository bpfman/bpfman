use std::{
    process::{Child, Command},
    thread::sleep,
    time::Duration,
};

use assert_cmd::prelude::*;
use log::{debug, info, LevelFilter};

mod tests;
use tests::IntegrationTest;

struct ChildGuard(Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        match self.0.kill() {
            Err(e) => println!("Could not kill child process: {}", e),
            Ok(_) => println!("Successfully killed child process"),
        }
    }
}

fn main() -> anyhow::Result<()> {
    let mut builder = env_logger::Builder::from_default_env();
    builder.filter_level(LevelFilter::Debug);
    builder.init();

    // Run the tests
    for t in inventory::iter::<IntegrationTest> {
        info!("Running {}", t.name);
        debug!("Starting bpfd");
        let mut cmd = Command::cargo_bin("bpfd")?;
        let c = cmd.spawn()?;
        let _guard = ChildGuard(c);
        // let bpfd start up
        sleep(Duration::from_secs(2));
        if let Err(e) = (t.test_fn)() {
            panic!("{}", e)
        };
    }
    Ok(())
}
