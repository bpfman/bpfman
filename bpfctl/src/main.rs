// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::collections::HashMap;

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
use hex::FromHex;
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
    /// Name of the ELF section from the object file.
    #[clap(short, long, default_value = "")]
    section_name: String,

    /// Optional: Global variables to be set when program is loaded. Format: <NAME>=<Hex Value>
    ///
    /// Multiple values supported by repeating the parameter.
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

    /// Required: Location of Program Bytecode to load. Either Local file (file:///<path>) or bytecode
    /// image URL (image://<container image url>)
    #[clap(short, long, value_parser)]
    location: String,

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
        #[clap(short, long)]
        direction: String,
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,
        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(long)]
        priority: i32,
        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Not yet supported for TC programs. May have unintended behavior if used.
        #[clap(long, num_args(1..))]
        proceed_on: Vec<String>,
    },
    Tracepoint {
        /// Required: The tracepoint to attach to. E.g sched/sched_switch
        #[clap(short, long)]
        tracepoint: String,
    },
}

#[derive(Clone, Debug)]
struct GlobalArg {
    /// Required: Name of global variable to set.
    name: String,
    /// Value of global variable.
    ///
    /// This is a very low level API.  User is responsible for ensuring that
    /// alignment and endianness are correct for target processor.
    value: Vec<u8>,
}

fn parse_global_arg(global_arg: &str) -> Result<GlobalArg, std::io::Error> {
    let mut parts = global_arg.split('=');

    let name_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;

    let value_str = parts.next().ok_or(std::io::ErrorKind::InvalidInput)?;
    let value = Vec::<u8>::from_hex(value_str).map_err(|_e| std::io::ErrorKind::InvalidInput)?;
    if value.is_empty() {
        return Err(std::io::ErrorKind::InvalidInput.into());
    }

    Ok(GlobalArg {
        name: name_str.to_string(),
        value,
    })
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
        Commands::Load(l) => {
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
                            direction: Direction::None as i32,
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
                    let attach_direction = match direction.as_str() {
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
                            direction: attach_direction as i32,
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

            let mut global_data: HashMap<String, Vec<u8>> = HashMap::new();

            if let Some(global) = &l.global {
                for g in global.iter() {
                    global_data.insert(g.name.to_string(), g.value.clone());
                }
            }

            let request = tonic::Request::new(LoadRequest {
                location: l.location.clone(),
                section_name: l.section_name.to_string(),
                program_type: prog_type as i32,
                attach_type,
                global_data,
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
            table.set_header(vec!["UUID", "Type", "Name", "Location", "Metadata"]);
            for r in response.results {
                let prog_type: ProgramType = r.program_type.try_into()?;
                match prog_type {
                    ProgramType::Xdp => {
                        if let Some(list_response::list_result::AttachType::NetworkMultiAttach(
                            NetworkMultiAttach {
                                priority,
                                iface,
                                position,
                                direction: _,
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
                            r.location,
                            format!(r#"{{ "priority": {priority}, "iface": "{iface}", "position": {position}, "proceed_on": {proceed_on} }}"#)
                        ]);
                        }
                    }
                    ProgramType::Tc => {
                        if let Some(list_response::list_result::AttachType::NetworkMultiAttach(
                            NetworkMultiAttach {
                                priority,
                                iface,
                                position,
                                direction,
                                proceed_on: _,
                            },
                        )) = r.attach_type
                        {
                            let attach_direction = match direction {
                                0 => Direction::None,
                                1 => Direction::Ingress,
                                2 => Direction::Egress,
                                other => bail!("{} is not a valid direction", other),
                            }
                            .as_str_name();
                            //attach_direction = attach_direction.as_str_name()
                            table.add_row(vec![
                                r.id.to_string(),
                                "tc".to_string(),
                                r.name,
                                r.location,
                                format!(r#"{{ "priority": {priority}, "iface": "{iface}", "position": {position}, direction: {attach_direction} }}"#)
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
                                r.location,
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
