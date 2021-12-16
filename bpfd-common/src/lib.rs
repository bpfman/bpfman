#![no_std]

pub const XDP_METADATA_SECTION: &str = "xdp_metadata";
pub const TC_METADATA_SECTION: &str = "tc_metadata";

pub const XDP_DISPATCHER_VERSION: u32 = 1;
pub const XDP_DISPATCHER_RETVAL: u32 = 31;

pub const TC_DISPATCHER_VERSION: u32 = 1;
pub const TC_DISPATCHER_RETVAL: i32 = 31;

pub const MAX_DISPATCHER_ACTIONS: usize = 10;

#[repr(C)]
pub struct XdpDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; MAX_DISPATCHER_ACTIONS],
}

#[repr(C)]
pub struct TcDispatcherConfig {
    pub num_progs_enabled: u8,
    pub chain_call_actions: [u32; MAX_DISPATCHER_ACTIONS],
    pub run_prios: [u32; MAX_DISPATCHER_ACTIONS],
}
