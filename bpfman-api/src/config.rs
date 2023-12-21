// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{collections::HashMap, str::FromStr};

use aya::programs::XdpFlags;
use serde::{Deserialize, Serialize};
use thiserror::Error;

#[derive(Debug, Deserialize, Default, Clone)]
pub struct Config {
    pub interfaces: Option<HashMap<String, InterfaceConfig>>,
    #[serde(default)]
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
}
