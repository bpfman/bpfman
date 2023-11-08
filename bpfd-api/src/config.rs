// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfd

use std::{collections::HashMap, str::FromStr};

use aya::programs::XdpFlags;
use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::util::directories::*;

#[derive(Debug, Deserialize, Default, Clone)]
pub struct Config {
    #[serde(default)]
    pub tls: TlsConfig,
    pub interfaces: Option<HashMap<String, InterfaceConfig>>,
    #[serde(default)]
    pub grpc: Grpc,
    pub signing: Option<SigningConfig>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct SigningConfig {
    pub allow_unsigned: bool,
}

impl Default for SigningConfig {
    fn default() -> Self {
        Self {
            // Allow unsigned programs by default
            allow_unsigned: true,
        }
    }
}

#[derive(Debug, Error)]
pub enum ConfigError {
    #[error("Error parsing config file: {0}")]
    ParseError(#[from] toml::de::Error),
}

impl FromStr for Config {
    type Err = ConfigError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        toml::from_str(s).map_err(ConfigError::ParseError)
    }
}

#[derive(Debug, Deserialize, Clone)]
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

#[derive(Debug, Deserialize, Copy, Clone)]
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

#[derive(Debug, Deserialize, Clone)]
pub struct Grpc {
    #[serde(default)]
    pub endpoints: Vec<Endpoint>,
}

impl Default for Grpc {
    fn default() -> Self {
        Self {
            endpoints: vec![Endpoint::default()],
        }
    }
}

#[derive(Debug, Deserialize, Clone)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum Endpoint {
    Tcp {
        #[serde(default = "default_address")]
        address: String,
        #[serde(default = "default_port")]
        port: u16,
        #[serde(default = "default_enabled")]
        enabled: bool,
    },
    Unix {
        #[serde(default = "default_unix")]
        path: String,
        #[serde(default = "default_enabled")]
        enabled: bool,
    },
}

impl Default for Endpoint {
    fn default() -> Self {
        Endpoint::Tcp {
            address: default_address(),
            port: default_port(),
            enabled: default_enabled(),
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
    STPATH_BPFD_SOCKET.to_string()
}

fn default_enabled() -> bool {
    true
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_config_from_invalid_string() {
        assert!(Config::from_str("i am a teapot").is_err());
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
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Tcp {
                address,
                port,
                enabled,
            } => {
                assert_eq!(address, &default_address());
                assert_eq!(port, &default_port());
                assert_eq!(enabled, &true);
            }
            _ => panic!("Failed to parse empty configuration"),
        }
    }

    #[test]
    fn test_config_endpoint_tcp_default() {
        let input = r#"
        [[grpc.endpoints]]
        type = "tcp"
        "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Tcp {
                address,
                port,
                enabled,
            } => {
                assert_eq!(address, &default_address());
                assert_eq!(port, &default_port());
                assert!(enabled);
            }
            _ => panic!("Failed to parse empty configuration"),
        }
    }

    #[test]
    fn test_config_endpoint_tcp_no_port() {
        let input = r#"
            [[grpc.endpoints]]
            type = "tcp"
            address = "127.0.0.1"
            "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Tcp {
                address,
                port,
                enabled,
            } => {
                assert_eq!(address, &"127.0.0.1");
                assert_eq!(port, &default_port());
                assert!(enabled);
            }
            _ => panic!("Failed to parse TCP endpoint"),
        }
    }

    #[test]
    fn test_config_endpoint_tcp_no_address() {
        let input = r#"
            [[grpc.endpoints]]
            type = "tcp"
            port = 50052
            "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Tcp {
                address,
                port,
                enabled,
            } => {
                assert_eq!(address, &default_address());
                assert_eq!(port, &50052);
                assert!(enabled);
            }
            _ => panic!("Failed to parse TCP endpoint"),
        }
    }

    #[test]
    fn test_config_endpoint_unix_default() {
        let input = r#"
            [[grpc.endpoints]]
            type = "unix"
            "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Unix { path, enabled } => {
                assert_eq!(path, &default_unix());
                assert!(enabled);
            }
            _ => panic!("Failed to parse Unix socket"),
        }
    }

    #[test]
    fn test_config_endpoint_unix() {
        let input = r#"
            [[grpc.endpoints]]
            type = "unix"
            path = "/tmp/socket"
            "#;

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 1);

        match endpoints.get(0).unwrap() {
            Endpoint::Unix { path, enabled } => {
                assert_eq!(path, "/tmp/socket");
                assert!(enabled);
            }
            _ => panic!("Failed to parse Unix socket"),
        }
    }

    #[test]
    fn test_config_endpoint() {
        let input = r#"
            [[grpc.endpoints]]
            type = "tcp"
            enabled = true
            address = "::1"
            port = 50051

            [[grpc.endpoints]]
            type = "tcp"
            enabled = false
            address = "127.0.0.1"
            port = 50051

            [[grpc.endpoints]]
            type = "unix"
            enabled = true
            path = "/run/bpfd/bpfd.sock"
        "#;

        let expected_endpoints: Vec<Endpoint> = vec![
            Endpoint::Tcp {
                address: String::from("::1"),
                port: 50051,
                enabled: true,
            },
            Endpoint::Tcp {
                address: String::from("127.0.0.1"),
                port: 50051,
                enabled: false,
            },
            Endpoint::Unix {
                path: String::from("/run/bpfd/bpfd.sock"),
                enabled: true,
            },
        ];

        let config: Config = toml::from_str(input).expect("error parsing toml input");
        let endpoints = config.grpc.endpoints;
        assert_eq!(endpoints.len(), 3);

        for (i, endpoint) in endpoints.iter().enumerate() {
            match endpoint {
                Endpoint::Unix { path, enabled } => {
                    if let Endpoint::Unix {
                        path: expected_path,
                        enabled: expected_enabled,
                    } = expected_endpoints.get(i).unwrap()
                    {
                        assert_eq!(path, expected_path);
                        assert_eq!(enabled, expected_enabled);
                    } else {
                        panic!("Mismatch on endpoint type");
                    }
                }
                Endpoint::Tcp {
                    address,
                    port,
                    enabled,
                } => {
                    if let Endpoint::Tcp {
                        address: expected_address,
                        port: expected_port,
                        enabled: expected_enabled,
                    } = expected_endpoints.get(i).unwrap()
                    {
                        assert_eq!(address, expected_address);
                        assert_eq!(port, expected_port);
                        assert_eq!(enabled, expected_enabled);
                    } else {
                        panic!("Mismatch on endpoint type");
                    }
                }
            }
        }
    }
}
