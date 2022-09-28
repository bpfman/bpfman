// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use log::info;
use nix::net::if_::{if_nameindex, if_nametoindex};

use crate::server::errors::BpfdError;

pub(crate) fn get_ifindex(iface: &str) -> Result<u32, BpfdError> {
    match if_nametoindex(iface) {
        Ok(index) => {
            info!("Map {} to {}", iface, index);
            Ok(index)
        }
        Err(_) => {
            info!("Unable to validate interface {}", iface);
            Err(BpfdError::InvalidInterface)
        }
    }
}

pub(crate) fn get_ifname(ifindex: u32) -> Result<String, BpfdError> {
    let ifaces = if_nameindex().map_err(|_| BpfdError::InvalidInterface)?;
    if let Some(i) = ifaces.into_iter().find(|i| i.index() == ifindex) {
        Ok(i.name().to_string_lossy().to_string())
    } else {
        Err(BpfdError::InvalidInterface)
    }
}
