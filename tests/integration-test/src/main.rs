use std::env;

use inventory::iter;

use log::{info, LevelFilter};
use tests::IntegrationTest;

mod tests;

fn main() -> anyhow::Result<()> {
    let mut builder = env_logger::Builder::from_default_env();
    builder.filter_level(LevelFilter::Debug);
    builder.init();

    let args: Vec<String> = env::args().collect();
    // Ignore the first arg, which is the function name
    let tests_to_run = &args[1..];
    let tests_to_run_len = tests_to_run.len();

    if tests_to_run_len > 0 {
        info!("Executing test case(s): {:?}", tests_to_run);
    } else {
        info!("Executing all test cases");
    }

    // Get the tests into a collection that can be sorted.
    let mut tests: Vec<<iter<IntegrationTest> as IntoIterator>::Item> =
        inventory::iter::<IntegrationTest>.into_iter().collect();

    // Sort the tests so we can excute them in the desired order
    tests.sort();

    // Run the tests
    for t in tests {
        let test_name: String = t
            .name
            .split("::")
            .collect::<Vec<&str>>()
            .pop()
            .expect("not a valid test name")
            .to_string();

        if tests_to_run_len == 0 || tests_to_run.contains(&test_name) {
            info!("Running {}", t.name);
            (t.test_fn)();
        }
    }
    Ok(())
}
