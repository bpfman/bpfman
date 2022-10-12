// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

pub mod certs;
pub mod util;
#[path = "bpfd.v1.rs"]
#[rustfmt::skip]
#[allow(clippy::all)]
pub mod v1;

use thiserror::Error;
use v1::{ProceedOn, ProgramType};

#[derive(Error, Debug)]
pub enum ParseError {
    #[error("{program} is not a valid program type")]
    InvalidProgramType { program: String },
    #[error("{proceedon} is not a valid proceed-on value")]
    InvalidProceedOn { proceedon: String },
}

impl ToString for ProgramType {
    fn to_string(&self) -> String {
        match &self {
            ProgramType::Xdp => "xdp".to_owned(),
            ProgramType::TcIngress => "tc_ingress".to_owned(),
            ProgramType::TcEgress => "tc_egress".to_owned(),
        }
    }
}

impl TryFrom<String> for ProgramType {
    type Error = ParseError;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "xdp" => ProgramType::Xdp,
            "tc_ingress" => ProgramType::TcIngress,
            "tc_egress" => ProgramType::TcEgress,
            program => {
                return Err(ParseError::InvalidProgramType {
                    program: program.to_string(),
                })
            }
        })
    }
}

impl ToString for ProceedOn {
    fn to_string(&self) -> String {
        match &self {
            ProceedOn::Aborted => "aborted".to_owned(),
            ProceedOn::Drop => "drop".to_owned(),
            ProceedOn::Pass => "pass".to_owned(),
            ProceedOn::Tx => "tx".to_owned(),
            ProceedOn::Redirect => "redirect".to_owned(),
            ProceedOn::DispatcherReturn => "dispatcher_return".to_owned(),
        }
    }
}

impl TryFrom<String> for ProceedOn {
    type Error = ParseError;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "aborted" => ProceedOn::Aborted,
            "drop" => ProceedOn::Drop,
            "pass" => ProceedOn::Pass,
            "tx" => ProceedOn::Tx,
            "redirect" => ProceedOn::Redirect,
            "dispatcher_return" => ProceedOn::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}

impl TryFrom<u32> for ProceedOn {
    type Error = ParseError;

    fn try_from(value: u32) -> Result<Self, Self::Error> {
        Ok(match value {
            0 => ProceedOn::Aborted,
            1 => ProceedOn::Drop,
            2 => ProceedOn::Pass,
            3 => ProceedOn::Tx,
            4 => ProceedOn::Redirect,
            31 => ProceedOn::DispatcherReturn,
            proceedon => {
                return Err(ParseError::InvalidProceedOn {
                    proceedon: proceedon.to_string(),
                })
            }
        })
    }
}
