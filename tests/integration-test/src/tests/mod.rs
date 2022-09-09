pub mod basic;

pub use integration_test_macros::integration_test;

#[derive(Debug)]
pub struct IntegrationTest {
    pub name: &'static str,
    pub test_fn: fn() -> anyhow::Result<()>,
}

inventory::collect!(IntegrationTest);
