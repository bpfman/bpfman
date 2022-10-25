// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs::create_dir_all, path::PathBuf};

use anyhow::Context;
use bpfd_api::{
    certs::get_tls_config,
    util::directories::*,
    v1::{
        loader_client::LoaderClient, ListRequest, LoadRequest, ProceedOn, ProgramType,
        UnloadRequest,
    },
};
use clap::{Parser, Subcommand};
mod config;
use comfy_table::Table;
use config::config_from_file;
use tonic::transport::{Channel, ClientTlsConfig};

const CN_NAME: &str = "bpfctl";

//#[derive(Parser, PartialEq, Eq)]
#[derive(Parser)]
#[clap(author, version, about, long_about = None)]
struct Cli {
    #[clap(subcommand)]
    command: Commands,
}

//#[derive(Subcommand, PartialEq, Eq)]
#[derive(Subcommand)]
enum Commands {
    Load {
        /// Required: Program to load.
        #[clap(parse(from_os_str))]
        path: PathBuf,
        /// Optional: Extract bytecode from container, signals <PATH> is a
        /// container image URL.
        #[clap(long, conflicts_with_all(&["program-type", "section-name"]))]
        from_image: bool,
        /// Required if "--from-image" is not present: BPF hook point.
        /// Possible values: [xdp]
        #[clap(
            short,
            long,
            default_value = "xdp",
            required_unless_present("from-image")
        )]
        program_type: String,
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,
        /// Required if "--from-image" is not present: Name of the ELF section from the object file.
        #[clap(short, long, default_value = "", required_unless_present("from-image"))]
        section_name: String,
        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(long)]
        priority: i32,
        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Possible values: [aborted, drop, pass, tx, redirect, dispatcher_return]
        /// Default values: pass and dispatcher_return
        #[clap(long, multiple = true)]
        proceed_on: Vec<String>,
    },
    Unload {
        #[clap(short, long)]
        /// Required: Interface to unload program from.
        iface: String,
        /// Required: UUID used to identify loaded program.
        id: String,
    },
    List {
        #[clap(short, long)]
        /// Required: Interface to list loaded programs from.
        iface: String,
    },
    Certs {},
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    env_logger::init();

    let config = config_from_file(CFGPATH_BPFCTL_CONFIG);
    let cli = Cli::parse();
    let create_certs = if matches!(&cli.command, Commands::Certs {}) {
        create_dir_all(CFGDIR_BPFCTL_CERTS).context("unable to create bpfctl certs directory")?;
        true
    } else {
        false
    };
    let (ca_cert, identity) = get_tls_config(
        &config.tls.ca_cert,
        &config.tls.key,
        &config.tls.cert,
        CN_NAME,
        false,
        create_certs,
    )
    .await
    .context("CA Cert File does not exist")?;

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
            let mut table = Table::new();
            table.load_preset(comfy_table::presets::NOTHING);
            table.set_header(vec![
                "Type",
                "Position",
                "UUID",
                "Name",
                "Priority",
                "Path",
                "Proceed-On",
            ]);
            for r in response.results {
                let proceed_on: Vec<String> = r
                    .proceed_on
                    .iter()
                    .map(|action| ProceedOn::try_from(*action as u32).unwrap().to_string())
                    .collect();
                table.add_row(vec![
                    format!("xdp ({})", response.xdp_mode),
                    r.position.to_string(),
                    r.id.to_string(),
                    r.name,
                    r.priority.to_string(),
                    r.path,
                    proceed_on.join(", "),
                ]);
            }
            println!("{table}");
        }
        Commands::Certs {} => {}
    }
    Ok(())
}
