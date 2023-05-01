// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

// XDP Defines
// pub (crate) const XDP_METADATA_SECTION: &str = "xdp_metadata";
// pub (crate) const XDP_DISPATCHER_VERSION: u32 = 1;
// pub (crate) const XDP_DISPATCHER_RETVAL: u32 = 31;
pub(crate) const MAX_DISPATCHER_ACTIONS: usize = 10;

#[derive(Copy, Clone, Debug)]
#[repr(C)]
pub(crate) struct XdpDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; MAX_DISPATCHER_ACTIONS],
}

unsafe impl aya::Pod for XdpDispatcherConfig {}

// TC Defines
// pub (crate) const TC_METADATA_SECTION: &str = "tc_metadata";
// pub (crate) const TC_DISPATCHER_VERSION: u32 = 1;
// pub (crate) const TC_DISPATCHER_RETVAL: u32 = 31;
pub(crate) const TC_MAX_DISPATCHER_ACTIONS: usize = 10;

#[derive(Copy, Clone, Debug)]
#[repr(C)]
pub(crate) struct TcDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; TC_MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; TC_MAX_DISPATCHER_ACTIONS],
}

unsafe impl aya::Pod for TcDispatcherConfig {}
