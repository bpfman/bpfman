// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::path::PathBuf;

use anyhow::Context;
use bpfd_api::{
    config::config_from_file,
    util::directories::*,
    v1::{
        list_response, load_request, loader_client::LoaderClient, Direction, ListRequest,
        LoadRequest, NetworkMultiAttach, ProceedOn, ProgramType, SingleAttach, UnloadRequest,
    },
};
use clap::{Parser, Subcommand};
use comfy_table::Table;
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
        /// Required: Program to load.
        #[clap(value_parser)]
        path: PathBuf,
        /// Optional: Extract bytecode from container, signals <PATH> is a
        /// container image URL.
        #[clap(long, conflicts_with_all(&["program_type", "section_name"]))]
        from_image: bool,
        /// Required if "--from-image" is not present: BPF hook point.
        /// Possible values: [xdp]
        #[clap(
            short,
            long,
            default_value = "xdp",
            required_unless_present("from_image")
        )]
        program_type: String,
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,
        /// Required if "--from-image" is not present: Name of the ELF section from the object file.
        #[clap(short, long, default_value = "", required_unless_present("from_image"))]
        section_name: String,
        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(long)]
        priority: i32,
        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Possible values: [aborted, drop, pass, tx, redirect, dispatcher_return]
        /// Default values: pass and dispatcher_return
        #[clap(long, num_args(1..))]
        proceed_on: Vec<String>,
    },
    Unload {
        /// Required: UUID used to identify loaded program.
        id: String,
    },
    List {},
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    env_logger::init();

    let cli = Cli::parse();

    let config = config_from_file(CFGPATH_BPFD_CONFIG);

    let ca_cert = tokio::fs::read(&config.tls.ca_cert)
        .await
        .context("CA Cert File does not exist")?;
    let ca_cert = Certificate::from_pem(ca_cert);
    let cert = tokio::fs::read(&config.tls.client_cert)
        .await
        .context("Cert File does not exist")?;
    let key = tokio::fs::read(&config.tls.client_key)
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

    match &cli.command {
        Commands::Load {
            path,
            from_image,
            program_type,
            iface,
            section_name,
            priority,
            proceed_on,
        } => {
            let path_str: String = path.to_string_lossy().to_string();
            let prog_type = ProgramType::try_from(program_type.to_string())?;

            let mut proc_on = Vec::new();
            if !proceed_on.is_empty() {
                for i in proceed_on.iter() {
                    let action = ProceedOn::try_from(i.to_string())?;
                    proc_on.push(action as i32);
                }
            }

            let request = tonic::Request::new(LoadRequest {
                path: path_str,
                from_image: *from_image,
                section_name: section_name.to_string(),
                program_type: prog_type as i32,
                direction: Direction::None as i32,
                attach_type: Some(load_request::AttachType::NetworkMultiAttach(
                    NetworkMultiAttach {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        proceed_on: proc_on,
                    },
                )),
            });
            let response = client.load(request).await?.into_inner();
            println!("{}", response.id);
        }
        Commands::Unload { id } => {
            let request = tonic::Request::new(UnloadRequest { id: id.to_string() });
            let _response = client.unload(request).await?.into_inner();
        }
        Commands::List {} => {
            let request = tonic::Request::new(ListRequest {});
            let response = client.list(request).await?.into_inner();
            let mut table = Table::new();
            table.load_preset(comfy_table::presets::NOTHING);
            table.set_header(vec!["UUID", "Type", "Name", "Path", "Metadata"]);
            for r in response.results {
                let prog_type: ProgramType = r.program_type.try_into()?;
                match prog_type {
                    ProgramType::Xdp => {
                        if let Some(list_response::list_result::AttachType::NetworkMultiAttach(
                            NetworkMultiAttach {
                                priority,
                                iface,
                                position,
                                proceed_on,
                            },
                        )) = r.attach_type
                        {
                            let proceed_on: Vec<String> = proceed_on
                                .iter()
                                .map(|action| {
                                    ProceedOn::try_from(*action as u32).unwrap().to_string()
                                })
                                .collect();
                            let proceed_on = proceed_on.join(", ");
                            table.add_row(vec![
                            r.id.to_string(),
                            "xdp".to_string(),
                            r.name,
                            r.path,
                            format!(r#"{{"priority": {priority}, "iface": "{iface}", "postiion": {position}, "proceed_on": {proceed_on} }}"#)
                        ]);
                        }
                    }
                    ProgramType::Tc => {
                        if let Some(list_response::list_result::AttachType::NetworkMultiAttach(
                            NetworkMultiAttach {
                                priority,
                                iface,
                                position,
                                proceed_on: _,
                            },
                        )) = r.attach_type
                        {
                            let direction = r.direction.to_string();
                            table.add_row(vec![
                        r.id.to_string(),
                        format!("tc-{direction}"),
                        r.name,
                        r.path,
                        format!(r#"{{"priority": {priority}, "iface": "{iface}", "postiion": {position} }}"#)
                    ]);
                        }
                    }
                    ProgramType::Tracepoint => {
                        if let Some(list_response::list_result::AttachType::SingleAttach(
                            SingleAttach { name },
                        )) = r.attach_type
                        {
                            table.add_row(vec![
                                r.id.to_string(),
                                "tracepoint".to_string(),
                                r.name,
                                r.path,
                                format!(r#"{{ "tracepoint": {name} }}"#),
                            ]);
                        }
                    }
                }
            }
            println!("{table}");
        }
    }
    Ok(())
}
