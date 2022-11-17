// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use log::info;
use nix::net::if_::if_nametoindex;

use crate::errors::BpfdError;

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
