// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

mod tc;
mod xdp;

use bpfman_api::{
    config::{InterfaceConfig, XdpMode},
    ProgramType,
};
use log::debug;
pub use tc::TcDispatcher;
use tokio::sync::mpsc::Sender;
pub use xdp::XdpDispatcher;

use crate::{
    command::{Direction, Program},
    errors::BpfmanError,
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
    ) -> Result<Dispatcher, BpfmanError> {
        debug!("Dispatcher::new()");
        let p = programs
            .first()
            .ok_or_else(|| BpfmanError::Error("No programs to load".to_string()))?;
        let if_index = p
            .if_index()?
            .ok_or_else(|| BpfmanError::Error("missing ifindex".to_string()))?;
        let if_name = p.if_name()?;
        let direction = p.direction()?;
        let xdp_mode = if let Some(c) = config {
            c.xdp_mode
        } else {
            XdpMode::Skb
        };
        let d = match p.kind() {
            ProgramType::Xdp => {
                let mut x = XdpDispatcher::new(xdp_mode, if_index, if_name.to_string(), revision)?;

                x.load(programs, old_dispatcher, image_manager).await?;
                Dispatcher::Xdp(x)
            }
            ProgramType::Tc => {
                let mut t = TcDispatcher::new(
                    direction.expect("missing direction"),
                    if_index,
                    if_name.to_string(),
                    revision,
                )?;

                t.load(programs, old_dispatcher, image_manager).await?;
                Dispatcher::Tc(t)
            }
            _ => return Err(BpfmanError::DispatcherNotRequired),
        };
        Ok(d)
    }

    pub(crate) fn delete(&mut self, full: bool) -> Result<(), BpfmanError> {
        debug!("Dispatcher::delete()");
        match self {
            Dispatcher::Xdp(d) => d.delete(full),
            Dispatcher::Tc(d) => d.delete(full),
        }
    }

    pub(crate) fn next_revision(&self) -> u32 {
        let current = match self {
            Dispatcher::Xdp(d) => d
                .get_revision()
                .expect("failed to get xdp_dispatcher revision"),
            Dispatcher::Tc(d) => d
                .get_revision()
                .expect("failed to get tc_dispatcher revision"),
        };
        current.wrapping_add(1)
    }

    pub(crate) fn if_name(&self) -> String {
        match self {
            Dispatcher::Xdp(d) => d
                .get_ifname()
                .expect("failed to get xdp_dispatcher if_name"),
            Dispatcher::Tc(d) => d.get_ifname().expect("failed to tc xdp_dispatcher if_name"),
        }
    }

    pub(crate) fn num_extensions(&self) -> usize {
        match self {
            Dispatcher::Xdp(d) => d
                .get_num_extensions()
                .expect("failed to get xdp_dispatcher num_extensions"),
            Dispatcher::Tc(d) => d
                .get_num_extensions()
                .expect("failed to get tc_dispatcher num_extensions"),
        }
    }
}

#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub(crate) enum DispatcherId {
    Xdp(DispatcherInfo),
    Tc(DispatcherInfo),
}

#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub(crate) struct DispatcherInfo(pub u32, pub Option<Direction>);
