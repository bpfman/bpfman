
#![no_std]

#[repr(C)]
#[derive(Clone, Copy)]
pub struct packet_log {
    pub src_address: u32,
    pub dst_address: u32,
    pub src_port: u16,
    pub dst_port: u16,
    pub protocol: u8,
    pub _pad: [u8; 3],
    
}

#[repr(C)]
#[derive(Copy, Clone)]
pub struct packet_five_tuple { 
    pub src_address: u32, 
    pub dst_address: u32,
    pub src_port: u16,
    pub dst_port: u16, 
    pub protocol: u8,
    pub _pad: [u8; 3],
}

#[cfg(feature = "user")]
unsafe impl aya::Pod for packet_log {}

#[cfg(feature = "user")]
unsafe impl aya::Pod for packet_five_tuple {}
