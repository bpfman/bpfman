// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

mod tc;
mod xdp;

use log::debug;
use sled::Db;
pub use tc::TcDispatcher;
pub use xdp::XdpDispatcher;

use crate::{
    config::{InterfaceConfig, RegistryConfig, XdpMode},
    errors::BpfmanError,
    oci_utils::image_manager::ImageManager,
    types::{Direction, Link},
    utils::bytes_to_string,
};

pub(crate) const TC_DISPATCHER_PREFIX: &str = "tc_dispatcher";
pub(crate) const XDP_DISPATCHER_PREFIX: &str = "xdp_dispatcher";

#[derive(Debug)]
pub(crate) enum Dispatcher {
    Xdp(XdpDispatcher),
    Tc(TcDispatcher),
}

impl Dispatcher {
    pub fn new(
        root_db: &Db,
        if_config: Option<&InterfaceConfig>,
        registry_config: &RegistryConfig,
        links: &mut [Link],
        revision: u32,
        old_dispatcher: Option<Dispatcher>,
        image_manager: &mut ImageManager,
    ) -> Result<Dispatcher, BpfmanError> {
        debug!("Dispatcher::new()");
        let l = links
            .first()
            .ok_or_else(|| BpfmanError::Error("No programs to load".to_string()))?;
        let if_index = l
            .ifindex()?
            .ok_or_else(|| BpfmanError::Error("missing ifindex".to_string()))?;
        let if_name = l.if_name()?;
        let direction = l.direction()?;
        let xdp_mode = if let Some(c) = if_config {
            c.xdp_mode()
        } else {
            &XdpMode::Drv
        };
        let d = match l {
            Link::Xdp(xdp_link) => {
                let mut x = XdpDispatcher::new(
                    root_db,
                    xdp_mode,
                    if_index,
                    if_name.to_string(),
                    xdp_link.get_nsid()?,
                    revision,
                )?;

                x.load(
                    root_db,
                    links,
                    old_dispatcher,
                    image_manager,
                    registry_config,
                    xdp_link.get_netns()?,
                )?;

                Dispatcher::Xdp(x)
            }
            Link::Tc(tc_link) => {
                let mut t = TcDispatcher::new(
                    root_db,
                    direction.expect("missing direction"),
                    if_index,
                    if_name.to_string(),
                    tc_link.get_nsid()?,
                    tc_link.get_netns()?,
                    revision,
                )?;

                t.load(
                    root_db,
                    links,
                    old_dispatcher,
                    image_manager,
                    registry_config,
                    tc_link.get_netns()?,
                )?;

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
pub(crate) struct DispatcherInfo(pub u64, pub u32, pub Option<Direction>);
