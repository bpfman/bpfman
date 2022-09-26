use log::{info, LevelFilter};
use tests::IntegrationTest;

mod tests;

fn main() -> anyhow::Result<()> {
    let mut builder = env_logger::Builder::from_default_env();
    builder.filter_level(LevelFilter::Debug);
    builder.init();

    // Run the tests
    for t in inventory::iter::<IntegrationTest> {
        info!("Running {}", t.name);
        if let Err(e) = (t.test_fn)() {
            panic!("{}", e)
        };
    }
    Ok(())
}
