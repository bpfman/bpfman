use std::path::PathBuf;

use clap::{Parser, Subcommand};
use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};
use thiserror::Error;
pub mod bpfd_api {
    tonic::include_proto!("bpfd");
}

use bpfd_api::{loader_client::LoaderClient, LoadRequest, ProgramType, UnloadRequest};

#[derive(Parser)]
#[clap(author, version, about, long_about = None)]
struct Cli {
    #[clap(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    Load {
        #[clap(parse(from_os_str))]
        path: PathBuf,
        #[clap(short, long)]
        program_type: String,
        #[clap(short, long)]
        iface: String,
        #[clap(short, long)]
        section_name: String,
        #[clap(long)]
        priority: i32,
    },
    Unload {
        #[clap(long)]
        iface: String,
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

    let cli = Cli::parse();
    match &cli.command {
        Commands::Load {
            path,
            program_type,
            iface,
            section_name,
            priority,
        } => {
            let path_str: String = path.to_string_lossy().to_string();
            let prog_type = ProgramType::try_from(program_type.to_string()).unwrap();
            let request = tonic::Request::new(LoadRequest {
                path: path_str,
                program_type: prog_type as i32,
                iface: iface.to_string(),
                section_name: section_name.to_string(),
                priority: *priority,
            });
            let response = client.load(request).await?.into_inner();
            println!("{}", response.id);
        }
        Commands::Unload { iface, id } => {
            println!("{}", id);
            let request = tonic::Request::new(UnloadRequest {
                iface: iface.to_string(),
                id: id.to_string(),
            });
            let _response = client.unload(request).await?.into_inner();
        }
    };
    Ok(())
}
