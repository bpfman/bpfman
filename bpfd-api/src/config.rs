// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, fs, path::Path};

use aya::programs::XdpFlags;
use log::{error, warn};
use serde::{Deserialize, Serialize};

use crate::util::directories::*;

#[derive(Debug, Deserialize, Default)]
pub struct Config {
    #[serde(default)]
    pub tls: TlsConfig,
    pub interfaces: Option<HashMap<String, InterfaceConfig>>,
    #[serde(default)]
    pub grpc: Grpc,
}

#[derive(Debug, Deserialize)]
pub struct TlsConfig {
    #[serde(default = "default_ca_cert")]
    pub ca_cert: String,
    #[serde(default = "default_cert")]
    pub cert: String,
    #[serde(default = "default_key")]
    pub key: String,
    #[serde(default = "default_client_cert")]
    pub client_cert: String,
    #[serde(default = "default_client_key")]
    pub client_key: String,
}

impl Default for TlsConfig {
    fn default() -> Self {
        Self {
            ca_cert: CFGPATH_CA_CERTS_PEM.to_string(),
            cert: CFGPATH_BPFD_CERTS_PEM.to_string(),
            key: CFGPATH_BPFD_CERTS_KEY.to_string(),
            client_cert: CFGPATH_BPFD_CLIENT_CERTS_PEM.to_string(),
            client_key: CFGPATH_BPFD_CLIENT_CERTS_KEY.to_string(),
        }
    }
}

fn default_ca_cert() -> String {
    CFGPATH_CA_CERTS_PEM.to_string()
}

fn default_cert() -> String {
    CFGPATH_BPFD_CERTS_PEM.to_string()
}

fn default_key() -> String {
    CFGPATH_BPFD_CERTS_KEY.to_string()
}

fn default_client_cert() -> String {
    CFGPATH_BPFD_CLIENT_CERTS_PEM.to_string()
}

fn default_client_key() -> String {
    CFGPATH_BPFD_CLIENT_CERTS_KEY.to_string()
}

#[derive(Debug, Deserialize)]
pub struct InterfaceConfig {
    pub xdp_mode: XdpMode,
}

#[derive(Debug, Serialize, Deserialize, Copy, Clone, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum XdpMode {
    Skb,
    Drv,
    Hw,
}

impl XdpMode {
    pub fn as_flags(&self) -> XdpFlags {
        match self {
            XdpMode::Skb => XdpFlags::SKB_MODE,
            XdpMode::Drv => XdpFlags::DRV_MODE,
            XdpMode::Hw => XdpFlags::HW_MODE,
        }
    }
}

impl ToString for XdpMode {
    fn to_string(&self) -> String {
        match self {
            XdpMode::Skb => "skb".to_string(),
            XdpMode::Drv => "drv".to_string(),
            XdpMode::Hw => "hw".to_string(),
        }
    }
}

#[derive(Debug, Deserialize, Default)]
pub struct Grpc {
    #[serde(default)]
    pub endpoint: Endpoint,
}

#[derive(Debug, Deserialize)]
pub struct Endpoint {
    #[serde(default = "default_address")]
    pub address: String,
    #[serde(default = "default_port")]
    pub port: u16,
    #[serde(default = "default_unix")]
    pub unix: String,
}

impl Default for Endpoint {
    fn default() -> Self {
        Endpoint {
            address: default_address(),
            port: default_port(),
            unix: default_unix(),
        }
    }
}

fn default_address() -> String {
    String::from("::1")
}

fn default_port() -> u16 {
    50051
}

fn default_unix() -> String {
    RTDIR_SOCKET.to_string()
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

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_config_from_invalid_path() {
        config_from_file("/tmp/bpfd_empty_config.toml");
    }

    #[test]
    fn test_config_single_iface() {
        let input = r#"
        [tls]
        ca_cert = "/path/to/ca/ca-cert.pem"
        cert = "/path/to/bpfd/cert.pem"
        key = "/path/to/bpfd/cert.key"
        client_cert = "/path/to/bpfd-client/cert.pem"
        client_key = "/path/to/bpfd-client/cert.key"

        [interfaces]
          [interfaces.eth0]
          xdp_mode = "drv"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        match config.interfaces {
            Some(i) => {
                assert!(i.contains_key("eth0"));
                assert_eq!(i.get("eth0").unwrap().xdp_mode, XdpMode::Drv)
            }
            None => panic!("expected interfaces to be present"),
        }
    }

    #[test]
    fn test_config_multiple_iface() {
        let input = r#"
        [tls]
        ca_cert = "/path/to/ca/ca-cert.pem"
        cert = "/path/to/bpfd/cert.pem"
        key = "/path/to/bpfd/cert.key"
        client_cert = "/path/to/bpfd-client/cert.pem"
        client_key = "/path/to/bpfd-client/cert.key"

        [interfaces]
          [interfaces.eth0]
          xdp_mode = "drv"
          [interfaces.eth1]
          xdp_mode = "hw"
          [interfaces.eth2]
          xdp_mode = "skb"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        match config.interfaces {
            Some(i) => {
                assert_eq!(i.len(), 3);
                assert!(i.contains_key("eth0"));
                assert_eq!(i.get("eth0").unwrap().xdp_mode, XdpMode::Drv);
                assert!(i.contains_key("eth1"));
                assert_eq!(i.get("eth1").unwrap().xdp_mode, XdpMode::Hw);
                assert!(i.contains_key("eth2"));
                assert_eq!(i.get("eth2").unwrap().xdp_mode, XdpMode::Skb);
            }
            None => panic!("expected interfaces to be present"),
        }
    }

    #[test]
    fn test_config_tls() {
        let input = r#"
        [tls]
        ca_cert = "/path/to/ca/ca-cert.pem"
        cert = "/path/to/bpfd/cert.pem"
        key = "/path/to/bpfd/cert.key"
        client_cert = "/path/to/bpfd-client/cert.pem"
        client_key = "/path/to/bpfd-client/cert.key"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.tls.ca_cert, "/path/to/ca/ca-cert.pem");
        assert_eq!(config.tls.cert, "/path/to/bpfd/cert.pem");
        assert_eq!(config.tls.key, "/path/to/bpfd/cert.key");
        assert_eq!(config.tls.client_cert, "/path/to/bpfd-client/cert.pem");
        assert_eq!(config.tls.client_key, "/path/to/bpfd-client/cert.key");
    }

    #[test]
    fn test_config_tls_missing_field() {
        let input = r#"
        [tls]
        ca_cert = "/path/to/ca/ca-cert.pem"
        cert = "/path/to/bpfd/cert.pem"
        key = "/path/to/bpfd/cert.key"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.tls.ca_cert, "/path/to/ca/ca-cert.pem");
        assert_eq!(config.tls.cert, "/path/to/bpfd/cert.pem");
        assert_eq!(config.tls.key, "/path/to/bpfd/cert.key");
        assert_eq!(
            config.tls.client_cert,
            CFGPATH_BPFD_CLIENT_CERTS_PEM.to_string()
        );
        assert_eq!(
            config.tls.client_key,
            CFGPATH_BPFD_CLIENT_CERTS_KEY.to_string()
        );
    }

    #[test]
    fn test_config_endpoint_default() {
        let input = r#"
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, default_address());
        assert_eq!(config.grpc.endpoint.port, default_port());
        assert_eq!(config.grpc.endpoint.unix, default_unix());
    }

    #[test]
    fn test_config_endpoint_no_endpoint() {
        let input = r#"
        [grpc]
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, default_address());
        assert_eq!(config.grpc.endpoint.port, default_port());
        assert_eq!(config.grpc.endpoint.unix, default_unix());
    }

    #[test]
    fn test_config_endpoint_empty_endpoint() {
        let input = r#"
        [grpc.endpoint]
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, default_address());
        assert_eq!(config.grpc.endpoint.port, default_port());
        assert_eq!(config.grpc.endpoint.unix, default_unix());
    }

    #[test]
    fn test_config_endpoint_no_port() {
        let input = r#"
        [grpc.endpoint]
        address = "127.0.0.1"
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, "127.0.0.1");
        assert_eq!(config.grpc.endpoint.port, default_port());
        assert_eq!(config.grpc.endpoint.unix, default_unix());
    }

    #[test]
    fn test_config_endpoint_no_address() {
        let input = r#"
        [grpc.endpoint]
        port = 50052
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, default_address());
        assert_eq!(config.grpc.endpoint.port, 50052);
        assert_eq!(config.grpc.endpoint.unix, default_unix());
    }

    #[test]
    fn test_config_endpoint_unix() {
        let input = r#"
        [grpc.endpoint]
        unix = "/tmp/socket"
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, default_address());
        assert_eq!(config.grpc.endpoint.port, default_port());
        assert_eq!(config.grpc.endpoint.unix, "/tmp/socket");
    }

    #[test]
    fn test_config_endpoint() {
        let input = r#"
        [grpc.endpoint]
        address = "127.0.0.1"
        port = 50052
        unix = "/tmp/socket"
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.grpc.endpoint.address, "127.0.0.1");
        assert_eq!(config.grpc.endpoint.port, 50052);
        assert_eq!(config.grpc.endpoint.unix, "/tmp/socket");
    }
}
