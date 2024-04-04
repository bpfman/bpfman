pub mod basic;
pub mod e2e;
pub mod utils;

pub use integration_test_macros::integration_test;

#[derive(Debug)]
pub struct IntegrationTest {
    pub name: &'static str,
    pub test_fn: fn(),
}

pub(crate) const RTDIR_FS_MAPS: &str = "/run/bpfman/fs/maps";
pub(crate) const RTDIR_FS_TC_INGRESS: &str = "/run/bpfman/fs/tc-ingress";
pub(crate) const RTDIR_FS_XDP: &str = "/run/bpfman/fs/xdp";
pub(crate) const RTDIR_FS_TC_EGRESS: &str = "/run/bpfman/fs/tc-egress";

inventory::collect!(IntegrationTest);
