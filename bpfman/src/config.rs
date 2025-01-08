// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use std::{collections::HashMap, str::FromStr};

use aya::programs::XdpFlags;
use serde::{Deserialize, Serialize};

use crate::{errors::ParseError, TC_DISPATCHER_IMAGE, XDP_DISPATCHER_IMAGE};

#[derive(Debug, Default, Deserialize, Clone)]
pub(crate) struct Config {
    interfaces: Option<HashMap<String, InterfaceConfig>>,
    #[serde(default)]
    signing: SigningConfig,
    #[serde(default)]
    database: DatabaseConfig,
    #[serde(default)]
    registry: RegistryConfig,
}

impl Config {
    pub(crate) fn interfaces(&self) -> &Option<HashMap<String, InterfaceConfig>> {
        &self.interfaces
    }

    pub(crate) fn signing(&self) -> &SigningConfig {
        &self.signing
    }

    pub(crate) fn database(&self) -> &DatabaseConfig {
        &self.database
    }

    pub(crate) fn registry(&self) -> &RegistryConfig {
        &self.registry
    }
}

#[derive(Debug, Deserialize, Clone)]
#[serde(default)]
pub struct SigningConfig {
    pub allow_unsigned: bool, // Allow unsigned programs
    pub verify_enabled: bool, // Enable verification of signed programs
}

impl Default for SigningConfig {
    fn default() -> Self {
        Self {
            // Whether to allow unsigned programs by default
            allow_unsigned: true,
            // Whether the signing of programs should be verified by default
            verify_enabled: true,
        }
    }
}

#[derive(Debug, Deserialize, Clone)]
#[serde(default)]
pub struct DatabaseConfig {
    pub max_retries: u32,
    pub millisec_delay: u64,
}

impl Default for DatabaseConfig {
    fn default() -> Self {
        Self {
            // Maximum number of times to attempt to open the database after a failed attempt
            max_retries: 10,
            // Number of milli-seconds to wait between failed database attempts
            millisec_delay: 1000,
        }
    }
}

#[derive(Debug, Deserialize, Clone)]
#[serde(default)]
pub struct RegistryConfig {
    pub xdp_dispatcher_image: String,
    pub tc_dispatcher_image: String,
}

impl Default for RegistryConfig {
    fn default() -> Self {
        Self {
            xdp_dispatcher_image: XDP_DISPATCHER_IMAGE.to_string(),
            tc_dispatcher_image: TC_DISPATCHER_IMAGE.to_string(),
        }
    }
}

impl FromStr for Config {
    type Err = ParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        toml::from_str(s).map_err(ParseError::ConfigParseError)
    }
}

#[derive(Debug, Deserialize, Copy, Clone)]
pub(crate) struct InterfaceConfig {
    xdp_mode: XdpMode,
}

impl InterfaceConfig {
    pub(crate) fn xdp_mode(&self) -> &XdpMode {
        &self.xdp_mode
    }
}

#[derive(Debug, Serialize, Deserialize, Copy, Clone, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub(crate) enum XdpMode {
    Skb,
    Drv,
    Hw,
}

impl XdpMode {
    pub(crate) fn as_flags(&self) -> XdpFlags {
        match self {
            XdpMode::Skb => XdpFlags::SKB_MODE,
            XdpMode::Drv => XdpFlags::DRV_MODE,
            XdpMode::Hw => XdpFlags::HW_MODE,
        }
    }
}

impl TryFrom<u32> for XdpMode {
    type Error = ParseError;

    fn try_from(mode: u32) -> Result<Self, Self::Error> {
        match mode {
            0 => Ok(XdpMode::Skb),
            1 => Ok(XdpMode::Drv),
            2 => Ok(XdpMode::Hw),
            _ => Err(ParseError::InvalidXdpMode {
                mode: mode.to_string(),
            }),
        }
    }
}

impl std::fmt::Display for XdpMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            XdpMode::Skb => write!(f, "skb"),
            XdpMode::Drv => write!(f, "drv"),
            XdpMode::Hw => write!(f, "hw"),
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

    #[test]
    fn test_config_incomplete_image_registry() {
        let input = r#"
        [registry]
          xdp_dispatcher_image = "foobar"
        "#;
        let expected = String::from("foobar");
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(config.registry.xdp_dispatcher_image, expected);
        assert_eq!(
            config.registry.tc_dispatcher_image,
            String::from(TC_DISPATCHER_IMAGE)
        )
    }

    #[test]
    fn test_config_invalid_image_registry() {
        let input = r#"
        [registry]
          xdeezpatcher_image = "foobar"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(
            config.registry.xdp_dispatcher_image,
            String::from(XDP_DISPATCHER_IMAGE)
        );
        assert_eq!(
            config.registry.tc_dispatcher_image,
            String::from(TC_DISPATCHER_IMAGE)
        )
    }
    #[test]
    fn test_config_no_image_registry() {
        let input = r#"
        "#;
        let config: Config = toml::from_str(input).expect("error parsing toml input");
        assert_eq!(
            config.registry.xdp_dispatcher_image,
            String::from(XDP_DISPATCHER_IMAGE)
        );
        assert_eq!(
            config.registry.tc_dispatcher_image,
            String::from(TC_DISPATCHER_IMAGE)
        )
    }
}
