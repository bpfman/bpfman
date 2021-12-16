#![no_std]
#![no_main]

use aya_bpf::{
    bindings::{__sk_buff, TC_ACT_OK},
    macros::classifier,
    programs::SkBuffContext,
    BpfContext,
};

use bpfd_common::*;

#[no_mangle]
static CONFIG: TcDispatcherConfig = TcDispatcherConfig {
    num_progs_enabled: 0,
    chain_call_actions: [0; MAX_DISPATCHER_ACTIONS],
    run_prios: [0; MAX_DISPATCHER_ACTIONS],
};

macro_rules! stub_program {
    ($prog:ident) => {
        #[classifier]
        pub fn $prog(_ctx: SkBuffContext) -> i32 {
            let ret = TC_DISPATCHER_RETVAL;
            // TODO: Check TcContext is valid, if not abort
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

#[classifier(name = "dispatcher")]
fn dispatcher(ctx: SkBuffContext) -> i32 {
    let cfg = &CONFIG as *const TcDispatcherConfig;
    let current_cfg = unsafe { core::ptr::read_volatile(&cfg) };
    let num_progs_enabled = unsafe { (*current_cfg).num_progs_enabled };

    macro_rules! stub_handler {
        ($n:literal, $fn:ident) => {
            if num_progs_enabled < ($n + 1) {
                return TC_ACT_OK;
            }
            let ret = $fn(ctx.as_ptr() as *mut __sk_buff);
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

    return TC_ACT_OK;
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unreachable!()
}
