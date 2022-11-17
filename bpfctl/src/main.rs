// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::path::PathBuf;

use anyhow::{bail, Context};
use bpfd_api::{
    config::config_from_file,
    util::directories::*,
    v1::{
        list_response, load_request, loader_client::LoaderClient, Direction, ListRequest,
        LoadRequest, NetworkMultiAttach, ProceedOn, ProgramType, SingleAttach, UnloadRequest,
    },
};
use clap::{Args, Parser, Subcommand};
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
    Load(Load),
    Unload { id: String },
    List,
}

#[derive(Args)]
struct Load {
    /// Optional: Extract bytecode from container, signals <PATH> is a
    /// container image URL.
    #[clap(long, conflicts_with("section_name"))]
    from_image: bool,

    /// Required if "--from-image" is not present: Name of the ELF section from the object file.
    #[clap(short, long, default_value = "", required_unless_present("from_image"))]
    section_name: String,

    /// Required: Program to load.
    #[clap(short, long, value_parser)]
    path: PathBuf,

    #[clap(subcommand)]
    command: LoadCommands,
}

#[derive(Subcommand)]
enum LoadCommands {
    Xdp {
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,
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
    Tc {
        /// Required: Direction to apply program. "ingress" or "egress"
        #[clap(long)]
        direction: String,
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,
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
    Tracepoint {
        /// Required: The tracepoint to attach to. E.g sched/sched_switch
        #[clap(short, long)]
        tracepoint: String,
    },
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
    let mut attach_direction = Direction::None;
    match &cli.command {
        Commands::Load(l) => {
            let path_str: String = l.path.to_string_lossy().to_string();
            let prog_type = match l.command {
                LoadCommands::Xdp { .. } => ProgramType::Xdp,
                LoadCommands::Tc { .. } => ProgramType::Tc,
                LoadCommands::Tracepoint { .. } => ProgramType::Tracepoint,
            };
            let attach_type = match &l.command {
                LoadCommands::Xdp {
                    iface,
                    priority,
                    proceed_on,
                } => {
                    let mut proc_on = Vec::new();
                    if !proceed_on.is_empty() {
                        for i in proceed_on.iter() {
                            let action = ProceedOn::try_from(i.to_string())?;
                            proc_on.push(action as i32);
                        }
                    }
                    Some(load_request::AttachType::NetworkMultiAttach(
                        NetworkMultiAttach {
                            priority: *priority,
                            iface: iface.to_string(),
                            position: 0,
                            proceed_on: proc_on,
                        },
                    ))
                }
                LoadCommands::Tc {
                    direction,
                    iface,
                    priority,
                    proceed_on,
                } => {
                    attach_direction = match direction.as_str() {
                        "ingress" => Direction::Ingress,
                        "egress" => Direction::Egress,
                        other => bail!("{} is not a valid direction", other),
                    };
                    let mut proc_on = Vec::new();
                    if !proceed_on.is_empty() {
                        for i in proceed_on.iter() {
                            let action = ProceedOn::try_from(i.to_string())?;
                            proc_on.push(action as i32);
                        }
                    }
                    Some(load_request::AttachType::NetworkMultiAttach(
                        NetworkMultiAttach {
                            priority: *priority,
                            iface: iface.to_string(),
                            position: 0,
                            proceed_on: proc_on,
                        },
                    ))
                }
                LoadCommands::Tracepoint { tracepoint } => {
                    Some(load_request::AttachType::SingleAttach(SingleAttach {
                        name: tracepoint.to_string(),
                    }))
                }
            };

            let request = tonic::Request::new(LoadRequest {
                path: path_str,
                from_image: l.from_image,
                section_name: l.section_name.to_string(),
                program_type: prog_type as i32,
                direction: attach_direction as i32,
                attach_type,
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
                                    format!(
                                        r#""{}""#,
                                        ProceedOn::try_from(*action as u32).unwrap().to_string()
                                    )
                                })
                                .collect();
                            let proceed_on = format!(r#"[{}]"#, proceed_on.join(", "));
                            table.add_row(vec![
                            r.id.to_string(),
                            "xdp".to_string(),
                            r.name,
                            r.path,
                            format!(r#"{{"priority": {priority}, "iface": "{iface}", "position": {position}, "proceed_on": {proceed_on} }}"#)
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
