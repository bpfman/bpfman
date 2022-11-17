pub mod basic;
pub mod utils;

pub use integration_test_macros::integration_test;

#[derive(Debug)]
pub struct IntegrationTest {
    pub name: &'static str,
    pub test_fn: fn(),
}

inventory::collect!(IntegrationTest);
