// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, os::unix::fs::PermissionsExt, str};

use bpfd_api::util::USRGRP_BPFD;
use log::info;
use nix::net::if_::if_nametoindex;
use users::get_group_by_name;

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

pub(crate) async fn set_map_permissions(directory: &str) {
    // Determine if User Group exists, if not, do nothing
    if get_group_by_name(USRGRP_BPFD).is_some() {
        // Iterate through the files in the provided directory
        for file in fs::read_dir(directory).unwrap() {
            // Set the permissions on the map such that members of the "bpfd" group can read/write
            if (tokio::fs::set_permissions(
                file.unwrap().path(),
                fs::Permissions::from_mode(0o0660),
            )
            .await)
                .is_err()
            {
                info!("Unable to set permissions on map. Continuing");
            }
        }
    }
}
