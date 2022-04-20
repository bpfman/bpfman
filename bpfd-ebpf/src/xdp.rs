#![no_std]
#![no_main]

use aya_bpf::{
    bindings::{xdp_action, xdp_md},
    macros::xdp,
    programs::XdpContext,
    BpfContext,
};

use bpfd_common::*;

#[no_mangle]
static CONFIG: XdpDispatcherConfig = XdpDispatcherConfig {
    num_progs_enabled: 0,
    chain_call_actions: [0; MAX_DISPATCHER_ACTIONS],
    run_prios: [0; MAX_DISPATCHER_ACTIONS],
};

macro_rules! stub_program {
    ($prog:ident) => {
        #[no_mangle]
        #[inline(never)]
        pub fn $prog(ctx: *mut ::aya_bpf::bindings::xdp_md) -> u32 {
            let ret = XDP_DISPATCHER_RETVAL;
            if ctx.is_null() {
                return xdp_action::XDP_ABORTED;
            }
            return ret;
        }
    };
}
stub_program!(prog0);
stub_program!(prog1);
stub_program!(prog2);
stub_program!(prog3);
stub_program!(prog4);
stub_program!(prog5);
stub_program!(prog6);
stub_program!(prog7);
stub_program!(prog8);
stub_program!(prog9);

#[xdp(name = "dispatcher")]
fn dispatcher(ctx: XdpContext) -> u32 {
    let cfg = &CONFIG as *const XdpDispatcherConfig;
    let current_cfg = unsafe { core::ptr::read_volatile(&cfg) };
    let num_progs_enabled = unsafe { (*current_cfg).num_progs_enabled } as usize;

    let mut ret = xdp_action::XDP_PASS;

    macro_rules! stub_handler {
        ($n:literal, $fn:ident) => {
            if num_progs_enabled < ($n + 1) {
                return ret;
            }
            ret = $fn(ctx.as_ptr() as *mut xdp_md);
            if (1 << ret) & unsafe { (*current_cfg).chain_call_actions[$n] } == 0 {
                return ret;
            };
        };
    }
    stub_handler!(0, prog0);
    stub_handler!(1, prog1);
    stub_handler!(2, prog2);
    stub_handler!(3, prog3);
    stub_handler!(4, prog4);
    stub_handler!(5, prog5);
    stub_handler!(6, prog6);
    stub_handler!(7, prog7);
    stub_handler!(8, prog8);
    stub_handler!(9, prog9);
    return xdp_action::XDP_PASS;
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unreachable!()
}
