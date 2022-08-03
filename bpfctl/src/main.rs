// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::path::PathBuf;

use anyhow::Context;
use bpfd::client::{ListRequest, LoadRequest, LoaderClient, ProceedOn, ProgramType, UnloadRequest};
use clap::{Parser, Subcommand};
use simplelog::{ColorChoice, ConfigBuilder, LevelFilter, TermLogger, TerminalMode};
mod config;
use config::config_from_file;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Identity};

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
        #[clap(long, multiple = true)]
        proceed_on: Vec<String>,
    },
    Unload {
        #[clap(short, long)]
        iface: String,
        id: String,
    },
    List {
        #[clap(short, long)]
        iface: String,
    },
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    TermLogger::init(
        LevelFilter::Info,
        ConfigBuilder::new()
            .set_target_level(LevelFilter::Error)
            .set_location_level(LevelFilter::Error)
            .build(),
        TerminalMode::Mixed,
        ColorChoice::Auto,
    )?;

    let config = config_from_file("/etc/bpfd/bpfctl.toml");

    let ca_cert = tokio::fs::read(&config.tls.ca_cert)
        .await
        .context("CA Cert File does not exist")?;
    let ca_cert = Certificate::from_pem(ca_cert);
    let cert = tokio::fs::read(&config.tls.cert)
        .await
        .context("Cert File does not exist")?;
    let key = tokio::fs::read(&config.tls.key)
        .await
        .context("Cert Key File does not exist")?;
    let identity = Identity::from_pem(cert, key);

    let tls_config = ClientTlsConfig::new()
        .domain_name("localhost")
        .ca_certificate(ca_cert)
        .identity(identity);
    let channel = Channel::from_static("http://[::1]:50051")
        .tls_config(tls_config)?
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
            proceed_on,
        } => {
            let path_str: String = path.to_string_lossy().to_string();
            let prog_type = ProgramType::try_from(program_type.to_string()).unwrap();
            let mut proc_on = Vec::new();
            if !proceed_on.is_empty() {
                for i in proceed_on.iter() {
                    let action = ProceedOn::try_from(i.to_string())?;
                    proc_on.push(action as i32);
                }
            }
            let request = tonic::Request::new(LoadRequest {
                path: path_str,
                program_type: prog_type as i32,
                iface: iface.to_string(),
                section_name: section_name.to_string(),
                priority: *priority,
                proceed_on: proc_on,
            });
            let response = client.load(request).await?.into_inner();
            println!("{}", response.id);
        }
        Commands::Unload { iface, id } => {
            let request = tonic::Request::new(UnloadRequest {
                iface: iface.to_string(),
                id: id.to_string(),
            });
            let _response = client.unload(request).await?.into_inner();
        }
        Commands::List { iface } => {
            let request = tonic::Request::new(ListRequest {
                iface: iface.to_string(),
            });
            let response = client.list(request).await?.into_inner();
            println!("{}\nxdp_mode: {}\n", iface, response.xdp_mode);
            for r in response.results {
                let proceed_on: Vec<String> = r
                    .proceed_on
                    .iter()
                    .map(|action| ProceedOn::try_from(*action as u32).unwrap().to_string())
                    .collect();
                println!(
                    "{}: {}\n\tname: \"{}\"\n\tpriority: {}\n\tpath: {}\n\tproceed-on: {}",
                    r.position,
                    r.id,
                    r.name,
                    r.priority,
                    r.path,
                    proceed_on.join(", ")
                );
            }
        }
    };
    Ok(())
}
