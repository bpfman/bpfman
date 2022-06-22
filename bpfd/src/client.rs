// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd
pub use crate::proto::bpfd_api::{
    loader_client::LoaderClient, ListRequest, ListResponse, LoadRequest, LoadResponse, ProgramType,
    UnloadRequest, UnloadResponse,
};
use thiserror::Error;

impl ToString for ProgramType {
    fn to_string(&self) -> String {
        match &self {
            ProgramType::Xdp => "xdp".to_owned(),
            ProgramType::TcIngress => "tc_ingress".to_owned(),
            ProgramType::TcEgress => "tc_egress".to_owned(),
        }
    }
}

#[derive(Error, Debug)]
pub enum ParseError {
    #[error("{program} is not a valid program type")]
    InvalidProgramType { program: String },
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
