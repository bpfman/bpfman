// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub(crate) mod args;
mod get;
mod image;
mod list;
mod load;
mod system;
mod table;
mod unload;
use std::fs;

use args::Commands;
use bpfman_api::{
    config::{self, Config},
    util::directories::CFGPATH_BPFMAN_CONFIG,
};
use get::execute_get;
use list::execute_list;
use log::warn;
use tokio::net::UnixStream;
use tonic::transport::{Channel, Endpoint, Uri};
use tower::service_fn;
use unload::execute_unload;

impl Commands {
    pub(crate) fn execute(&self) -> Result<(), anyhow::Error> {
        let mut config = if let Ok(c) = fs::read_to_string(CFGPATH_BPFMAN_CONFIG) {
            c.parse().unwrap_or_else(|_| {
                warn!("Unable to parse config file, using defaults");
                Config::default()
            })
        } else {
            warn!("Unable to read config file, using defaults");
            Config::default()
        };

        match self {
            Commands::Load(l) => l.execute(&mut config),
            Commands::Unload(args) => execute_unload(args, &mut config),
            Commands::List(args) => execute_list(args, &mut config),
            Commands::Get(args) => execute_get(args, &mut config),
            Commands::Image(i) => i.execute(&mut config),
            Commands::System(s) => s.execute(&config),
        }
    }
}

fn select_channel(config: &mut Config) -> Option<Channel> {
    let candidate = config
        .grpc
        .endpoints
        .iter_mut()
        .find(|e| matches!(e, config::Endpoint::Unix { path: _, enabled } if *enabled));
    if candidate.is_none() {
        warn!("No enabled unix endpoints found in config");
        return None;
    }
    let path = match candidate.as_ref().unwrap() {
        config::Endpoint::Unix { path, enabled: _ } => path.clone(),
    };

    let address = Endpoint::try_from(format!("unix:/{path}"));
    if let Err(e) = address {
        warn!("Failed to parse unix endpoint: {e:?}");
        if let Some(config::Endpoint::Unix { path: _, enabled }) = candidate {
            *enabled = false;
        }
        return select_channel(config);
    };
    let address = address.unwrap();
    let channel = address
        .connect_with_connector_lazy(service_fn(move |_: Uri| UnixStream::connect(path.clone())));
    Some(channel)
}
