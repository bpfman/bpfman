// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, path::Path};

use bpfd_api::util::directories::*;
use log::{error, warn};
use serde::Deserialize;

#[derive(Debug, Deserialize, Default)]
pub struct Config {
    pub tls: TlsConfig,
}

#[derive(Debug, Deserialize)]
pub struct TlsConfig {
    pub ca_cert: String,
    pub cert: String,
    pub key: String,
}

impl Default for TlsConfig {
    fn default() -> Self {
        Self {
            ca_cert: CFGPATH_CA_CERTS_PEM.to_string(),
            cert: CFGPATH_BPFCTL_CERTS_PEM.to_string(),
            key: CFGPATH_BPFCTL_CERTS_KEY.to_string(),
        }
    }
}

pub fn config_from_file<P: AsRef<Path>>(path: P) -> Config {
    if let Ok(contents) = fs::read_to_string(path) {
        toml::from_str(&contents).unwrap_or_else(|e| {
            error!("Error reading config file. Using default. {}", e);
            Config::default()
        })
    } else {
        warn!("No config file provided. Using default");
        Config::default()
    }
}
