#![no_std]
#![no_main]

use aya_bpf::{bindings::xdp_action, macros::xdp, programs::XdpContext};

#[xdp(name = "drop")]
fn drop(_ctx: XdpContext) -> u32 {
    xdp_action::XDP_DROP
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unreachable!()
}
