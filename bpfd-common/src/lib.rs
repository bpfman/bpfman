// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

#![no_std]

// XDP Defines
pub const XDP_METADATA_SECTION: &str = "xdp_metadata";
pub const XDP_DISPATCHER_VERSION: u32 = 1;
pub const XDP_DISPATCHER_RETVAL: u32 = 31;
pub const MAX_DISPATCHER_ACTIONS: usize = 10;

#[derive(Copy, Clone, Debug)]
#[repr(C)]
pub struct XdpDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; MAX_DISPATCHER_ACTIONS],
}

#[cfg(feature = "user")]
unsafe impl aya::Pod for XdpDispatcherConfig {}

// TC Defines
pub const TC_METADATA_SECTION: &str = "tc_metadata";
pub const TC_DISPATCHER_VERSION: u32 = 1;
pub const TC_DISPATCHER_RETVAL: u32 = 31;
pub const TC_MAX_DISPATCHER_ACTIONS: usize = 10;

#[derive(Copy, Clone, Debug)]
#[repr(C)]
pub struct TcDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; TC_MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; TC_MAX_DISPATCHER_ACTIONS],
}

#[cfg(feature = "user")]
unsafe impl aya::Pod for TcDispatcherConfig {}
