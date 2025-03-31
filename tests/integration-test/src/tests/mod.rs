pub(crate) const RTDIR_FS_TC_INGRESS: &str = "/run/bpfman/fs/tc-ingress";
pub(crate) const RTDIR_FS_XDP: &str = "/run/bpfman/fs/xdp";
pub(crate) const RTDIR_FS_TC_EGRESS: &str = "/run/bpfman/fs/tc-egress";
mod basic;
mod e2e;
mod error;
mod utils;
