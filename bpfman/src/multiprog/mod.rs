// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

mod tc;
mod xdp;

use log::debug;
use sled::Db;
pub use tc::TcDispatcher;
pub use xdp::XdpDispatcher;

use crate::{
    config::{InterfaceConfig, XdpMode},
    errors::BpfmanError,
    oci_utils::image_manager::ImageManager,
    types::{Direction, Program, ProgramType},
    utils::bytes_to_string,
};

pub(crate) const TC_DISPATCHER_PREFIX: &str = "tc_dispatcher_";
pub(crate) const XDP_DISPATCHER_PREFIX: &str = "xdp_dispatcher_";

#[derive(Debug)]
pub(crate) enum Dispatcher {
    Xdp(XdpDispatcher),
    Tc(TcDispatcher),
}

impl Dispatcher {
    pub async fn new(
        root_db: &Db,
        config: Option<&InterfaceConfig>,
        programs: &mut [Program],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
        image_manager: &mut ImageManager,
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
            c.xdp_mode()
        } else {
            &XdpMode::Drv
        };
        let d = match p.kind() {
            ProgramType::Xdp => {
                let mut x =
                    XdpDispatcher::new(root_db, xdp_mode, if_index, if_name.to_string(), revision)?;

                if let Err(res) = x
                    .load(root_db, programs, old_dispatcher, image_manager)
                    .await
                {
                    let _ = x.delete(root_db, true);
                    return Err(res);
                }
                Dispatcher::Xdp(x)
            }
            ProgramType::Tc => {
                let mut t = TcDispatcher::new(
                    root_db,
                    direction.expect("missing direction"),
                    if_index,
                    if_name.to_string(),
                    revision,
                )?;

                if let Err(res) = t
                    .load(root_db, programs, old_dispatcher, image_manager)
                    .await
                {
                    let _ = t.delete(root_db, true);
                    return Err(res);
                }
                Dispatcher::Tc(t)
            }
            _ => return Err(BpfmanError::DispatcherNotRequired),
        };
        Ok(d)
    }

    pub(crate) fn new_from_db(db_tree: sled::Tree) -> Dispatcher {
        if bytes_to_string(&db_tree.name()).contains("xdp") {
            Dispatcher::Xdp(XdpDispatcher::new_from_db(db_tree))
        } else {
            Dispatcher::Tc(TcDispatcher::new_from_db(db_tree))
        }
    }

    pub(crate) fn delete(&mut self, root_db: &Db, full: bool) -> Result<(), BpfmanError> {
        debug!("Dispatcher::delete()");
        match self {
            Dispatcher::Xdp(d) => d.delete(root_db, full),
            Dispatcher::Tc(d) => d.delete(root_db, full),
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
