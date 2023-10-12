// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{
    convert::{TryFrom, TryInto},
    sync::Arc,
};

use aya::{
    include_bytes_aligned, maps::perf::AsyncPerfEventArray, programs::TracePoint,
    util::online_cpus, Bpf,
};
use bytes::BytesMut;
use log::{debug, trace};
// use tokio::sync::Mutex;
use tokio::sync::mpsc::Sender;
use tokio::sync::Mutex;

use crate::serve::shutdown_handler;

// Assuming DEV_NAME_MAX_LEN and KIND_NAME_MAX_LEN are defined as constants
const DEV_NAME_MAX_LEN: usize = 64;
const KIND_NAME_MAX_LEN: usize = 64;

static QDISC_BYTES: &[u8] = include_bytes_aligned!("../../.output/tc_qdisc.bpf.o");

#[derive(Copy, Clone)]
#[repr(C)]
pub(crate) struct QdiscEvent {
    pub dev: [u8; DEV_NAME_MAX_LEN],
    pub kind: [u8; KIND_NAME_MAX_LEN],
}

pub(crate) struct OnQdiscDestroyEvent {}

impl OnQdiscDestroyEvent {
    #[allow(unreachable_code)]
    pub async fn run(qdisc_destroy_event: Arc<Mutex<Sender<QdiscEvent>>>) {
        debug!("QdiscDestroyObserver::start()");

        let mut bpf: Bpf = Bpf::load(QDISC_BYTES).unwrap();

        let program: &mut TracePoint = bpf
            .program_mut("tp_clsact_qdisc_destroy")
            .unwrap()
            .try_into()
            .unwrap();

        program.load().unwrap();
        debug!("QdiscDestroyObserver::start() program loaded");

        program.attach("qdisc", "qdisc_destroy").unwrap();
        debug!("QdiscDestroyObserver::start() program attached ");

        let mut perf_array: AsyncPerfEventArray<aya::maps::MapData> =
            AsyncPerfEventArray::try_from(bpf.take_map("perf_event_qdisc").unwrap()).unwrap();
        let cpus = online_cpus().unwrap();
        let num_cpus = cpus.len();

        for cpu_id in cpus {
            // open a separate perf buffer for each cpu
            let mut buf = perf_array.open(cpu_id, None).unwrap();

            trace!("QdiscDestroyObserver::start() perf_array opened");
            let qdisc_destroy_event_clone = qdisc_destroy_event.clone();

            // process each perf buffer in a separate task
            tokio::task::spawn(async move {
                let mut buffers = (0..num_cpus)
                    .map(|_| BytesMut::with_capacity(1024))
                    .collect::<Vec<_>>();

                loop {
                    // wait for events
                    trace!("QdiscDestroyObserver::start() wait for events");
                    let events = buf.read_events(&mut buffers).await?;

                    trace!("QdiscDestroyObserver::start() read events");
                    for buf in buffers.iter_mut().take(events.read) {
                        // TODO reuse the same buffer instead of allocating a new one.
                        // The *const QdiscEvent does not implement Send.
                        // let ptr = buf.as_ptr() as *const QdiscEvent;
                        // let qdisc_event = unsafe { read_unaligned(ptr) };

                        // Copying data from the raw buffer to a safe structure
                        let qdisc_event_data = buf[0..std::mem::size_of::<QdiscEvent>()].to_vec();
                        let qdisc_event: QdiscEvent =
                            unsafe { std::ptr::read(qdisc_event_data.as_ptr() as *const _) };

                        qdisc_destroy_event_clone
                            .lock()
                            .await
                            .send(qdisc_event)
                            .await
                            .unwrap();
                    }
                }

                Ok::<_, anyhow::Error>(())
            });
        }

        shutdown_handler().await;
    }
}
