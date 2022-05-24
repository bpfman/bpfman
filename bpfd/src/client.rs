use std::path::PathBuf;

use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};
use structopt::StructOpt;
use thiserror::Error;

pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

use bpfd_api::{loader_client::LoaderClient, LoadRequest, ProgramType, UnloadRequest};

#[derive(StructOpt)]
#[structopt(name = "bpfctl", about = "the bpf program loading daemon")]
pub struct Options {
    #[structopt(subcommand)]
    command: Command,
}

#[derive(StructOpt)]
enum Command {
    Load {
        #[structopt(parse(from_os_str))]
        path: PathBuf,
        #[structopt(short)]
        program_type: String,
        #[structopt(short)]
        iface: String,
        #[structopt(short)]
        section_name: String,
        #[structopt(long)]
        priority: i32,
    },
    Unload {
        #[structopt(short)]
        id: String,
    },
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

#[derive(Error, Debug)]
pub enum BpfctlError {
    #[error("{program} is not a valid program type")]
    InvalidProgramType { program: String },
}

impl TryFrom<String> for ProgramType {
    type Error = BpfctlError;

    fn try_from(value: String) -> Result<Self, Self::Error> {
        Ok(match value.as_str() {
            "xdp" => ProgramType::Xdp,
            "tc_ingress" => ProgramType::TcIngress,
            "tc_egress" => ProgramType::TcEgress,
            program => {
                return Err(BpfctlError::InvalidProgramType {
                    program: program.to_string(),
                })
            }
        })
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    TermLogger::init(
        LevelFilter::Info,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .build(),
        TerminalMode::Mixed,
        ColorChoice::Auto,
    )?;
    let channel = tonic::transport::Channel::from_static("http://[::1]:50051")
        .connect()
        .await?;

    let mut client = LoaderClient::new(channel);

    let opts = Options::from_args();
    match opts.command {
        Command::Load {
            path,
            program_type,
            iface,
            section_name,
            priority,
        } => {
            let path_str: String = path.to_string_lossy().to_string();
            let prog_type = ProgramType::try_from(program_type).unwrap();
            let request = tonic::Request::new(LoadRequest {
                path: path_str,
                program_type: prog_type as i32,
                iface,
                section_name,
                priority,
            });
            let response = client.load(request).await?.into_inner();
            println!("{}", response.id);
        }
        Command::Unload { id } => {
            println!("{}", id);
            let request = tonic::Request::new(UnloadRequest { id });
            let response = client.unload(request).await?.into_inner();
            println!("RESPONSE={:?}", response);
        }
    };
    Ok(())
}
