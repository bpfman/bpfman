// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfd

mod tc;
mod xdp;

use bpfd_api::{
    config::{InterfaceConfig, XdpMode},
    ProgramType,
};
use log::debug;
pub use tc::TcDispatcher;
use tokio::sync::mpsc::Sender;
pub use xdp::XdpDispatcher;

use crate::{
    command::{Direction, Program},
    errors::BpfdError,
    oci_utils::image_manager::Command as ImageManagerCommand,
};

pub(crate) enum Dispatcher {
    Xdp(XdpDispatcher),
    Tc(TcDispatcher),
}

impl Dispatcher {
    pub async fn new(
        config: Option<&InterfaceConfig>,
        programs: &mut [&mut Program],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
        image_manager: Sender<ImageManagerCommand>,
    ) -> Result<Dispatcher, BpfdError> {
        debug!("Dispatcher::new()");
        let p = programs
            .first()
            .ok_or_else(|| BpfdError::Error("No programs to load".to_string()))?;
        let if_index = p
            .if_index()
            .ok_or_else(|| BpfdError::Error("missing ifindex".to_string()))?;
        let if_name = p
            .if_name()
            .ok_or_else(|| BpfdError::Error("missing ifname".to_string()))?;
        let direction = p.direction();
        let xdp_mode = if let Some(c) = config {
            c.xdp_mode
        } else {
            XdpMode::Skb
        };
        let d = match p.kind() {
            ProgramType::Xdp => {
                let x = XdpDispatcher::new(
                    xdp_mode,
                    &if_index,
                    if_name,
                    programs,
                    revision,
                    old_dispatcher,
                    image_manager,
                )
                .await?;
                Dispatcher::Xdp(x)
            }
            ProgramType::Tc => {
                let direction =
                    direction.ok_or_else(|| BpfdError::Error("direction required".to_string()))?;
                let t = TcDispatcher::new(
                    direction,
                    &if_index,
                    if_name,
                    programs,
                    revision,
                    old_dispatcher,
                    image_manager,
                )
                .await?;
                Dispatcher::Tc(t)
            }
            _ => return Err(BpfdError::DispatcherNotRequired),
        };
        Ok(d)
    }

    pub(crate) fn delete(&mut self, full: bool) -> Result<(), BpfdError> {
        debug!("Dispatcher::delete()");
        match self {
            Dispatcher::Xdp(d) => d.delete(full),
            Dispatcher::Tc(d) => d.delete(full),
        }
    }

    pub(crate) fn next_revision(&self) -> u32 {
        let current = match self {
            Dispatcher::Xdp(d) => d.revision(),
            Dispatcher::Tc(d) => d.revision(),
        };
        current.wrapping_add(1)
    }

    pub(crate) fn if_name(&self) -> String {
        match self {
            Dispatcher::Xdp(d) => d.if_name(),
            Dispatcher::Tc(d) => d.if_name(),
        }
    }

    pub(crate) fn num_extensions(&self) -> usize {
        match self {
            Dispatcher::Xdp(d) => d.num_extensions(),
            Dispatcher::Tc(d) => d.num_extensions(),
        }
    }
}

#[derive(Debug, Hash, Eq, PartialEq)]
pub(crate) enum DispatcherId {
    Xdp(DispatcherInfo),
    Tc(DispatcherInfo),
}

#[derive(Debug, Hash, Eq, PartialEq)]
pub(crate) struct DispatcherInfo(pub u32, pub Option<Direction>);
