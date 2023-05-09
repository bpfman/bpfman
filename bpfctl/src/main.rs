// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, fs, net::SocketAddr, str};

use anyhow::{bail, Context};
use base64::{engine::general_purpose, Engine as _};
use bpfd_api::{
    config::Config,
    util::directories::*,
    v1::{
        list_response,
        load_request::{self, AttachInfo},
        load_request_common,
        loader_client::LoaderClient,
        BytecodeImage, ListRequest, LoadRequest, LoadRequestCommon, TcAttachInfo,
        TracepointAttachInfo, UnloadRequest, XdpAttachInfo,
    },
    ImagePullPolicy, ProgramType, TcProceedOn, XdpProceedOn,
};
use clap::{Args, Parser, Subcommand};
use comfy_table::Table;
use hex::FromHex;
use itertools::Itertools;
use log::{debug, info, warn};
use tokio::net::UnixStream;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Endpoint, Identity, Uri};
use tower::service_fn;

#[derive(Parser)]
#[clap(author, version, about, long_about = None)]
struct Cli {
    #[clap(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Load a BPF program from a local .o file.
    LoadFromFile(LoadFileArgs),
    /// Load a BPF program packaged in a OCI container image from a given registry.
    LoadFromImage(LoadImageArgs),
    /// Unload a BPF program using the UUID.
    Unload { id: String },
    /// List all BPF programs loaded via bpfd.
    List,
}

#[derive(Args)]
struct LoadFileArgs {
    /// Optional: Program uuid to be used by bpfd. If not
    /// specified, bpfd will generate a uuid.
    #[clap(short, long)]
    id: Option<String>,

    /// Name of the ELF section from the object file.
    #[clap(short, long, default_value = "")]
    section_name: String,

    /// Optional: Global variables to be set when program is loaded. Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

    /// Required: Location of Local bytecode file
    path: String,

    #[clap(subcommand)]
    command: LoadCommands,
}

#[derive(Args)]
struct LoadImageArgs {
    /// Optional: Program uuid to be used by bpfd. If not
    /// specified, bpfd will generate a uuid.
    #[clap(short, long)]
    id: Option<String>,

    /// Name of the ELF section from the object file.
    #[clap(short, long, default_value = "")]
    section_name: String,

    /// Required: Container Image URL
    #[clap(long)]
    image_url: String,

    /// ImagePullPolicy defaults to 'IfNotPresent'
    #[clap(long, default_value = "IfNotPresent")]
    image_pull_policy: String,

    /// registry auth for authenticating with the specified image registry this should
    /// be base64 encoded from the '<username>:<password>' string just like it's stored
    /// in the docker/podman host config.
    #[clap(short, long)]
    registry_auth: Option<String>,

    /// Optional: Global variables to be set when program is loaded. Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

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
        /// Possible values: [unspec, ok, reclassify, shot, pipe, stolen, queued,
        /// repeat, redirect, trap, dispatcher_return]
        /// Default values: ok, pipe, and dispatcher_return
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

struct ProgTable(Table);

impl ProgTable {
    fn new() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec!["UUID", "Type", "Name", "Location", "Metadata"]);
        ProgTable(table)
    }

    fn add_row(
        &mut self,
        uuid: String,
        type_: String,
        name: String,
        location: String,
        metadata: String,
    ) {
        self.0.add_row(vec![uuid, type_, name, location, metadata]);
    }

    fn add_response_prog(&mut self, r: list_response::ListResult) -> anyhow::Result<()> {
        let prog_type: ProgramType = r.program_type.try_into()?;
        match prog_type {
            ProgramType::Xdp => {
                if let Some(list_response::list_result::AttachInfo::XdpAttachInfo(
                    XdpAttachInfo {
                        priority,
                        iface,
                        position,
                        proceed_on,
                    },
                )) = r.attach_info
                {
                    let proc_on = match XdpProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };
                    self.add_row(r.id.to_string(),
				  "xdp".to_string(),
				  r.section_name.unwrap(),
				  r.location.unwrap().to_string(),
				  format!(r#"{{ priority: {priority}, iface: {iface}, position: {position}, proceed_on: {proc_on} }}"#));
                }
            }
            ProgramType::Tc => {
                if let Some(list_response::list_result::AttachInfo::TcAttachInfo(TcAttachInfo {
                    priority,
                    iface,
                    position,
                    direction,
                    proceed_on,
                })) = r.attach_info
                {
                    let proc_on = match TcProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };
                    self.add_row(r.id.to_string(),
				  "tc".to_string(),
				  r.section_name.unwrap(),
				  r.location.unwrap().to_string(),
				  format!(r#"{{ priority: {priority}, iface: {iface}, position: {position}, direction: {direction}, proceed_on: {proc_on} }}"#))
                }
            }
            ProgramType::Tracepoint => {
                if let Some(list_response::list_result::AttachInfo::TracepointAttachInfo(
                    TracepointAttachInfo { tracepoint },
                )) = r.attach_info
                {
                    self.add_row(
                        r.id.to_string(),
                        "tracepoint".to_string(),
                        r.section_name.unwrap(),
                        r.location.unwrap().to_string(),
                        format!(r#"{{ tracepoint: {tracepoint} }}"#),
                    );
                }
            }
            // skip unknown program types
            _ => {}
        }
        Ok(())
    }
}

impl std::fmt::Display for ProgTable {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl LoadCommands {
    fn get_prog_type(&self) -> ProgramType {
        match self {
            LoadCommands::Xdp { .. } => ProgramType::Xdp,
            LoadCommands::Tc { .. } => ProgramType::Tc,
            LoadCommands::Tracepoint { .. } => ProgramType::Tracepoint,
        }
    }

    fn get_attach_type(&self) -> Result<Option<AttachInfo>, anyhow::Error> {
        match self {
            LoadCommands::Xdp {
                iface,
                priority,
                proceed_on,
            } => {
                let proc_on = match XdpProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(load_request::AttachInfo::XdpAttachInfo(
                    XdpAttachInfo {
                        priority: *priority,
                        iface: iface.to_string(),
                        position: 0,
                        proceed_on: proc_on.as_action_vec(),
                    },
                )))
            }
            LoadCommands::Tc {
                direction,
                iface,
                priority,
                proceed_on,
            } => {
                match direction.as_str() {
                    "ingress" | "egress" => (),
                    other => bail!("{} is not a valid direction", other),
                };
                let proc_on = match TcProceedOn::from_strings(proceed_on) {
                    Ok(p) => p,
                    Err(e) => bail!("error parsing proceed_on {e}"),
                };
                Ok(Some(load_request::AttachInfo::TcAttachInfo(TcAttachInfo {
                    priority: *priority,
                    iface: iface.to_string(),
                    position: 0,
                    direction: direction.to_string(),
                    proceed_on: proc_on.as_action_vec(),
                })))
            }
            LoadCommands::Tracepoint { tracepoint } => Ok(Some(
                load_request::AttachInfo::TracepointAttachInfo(TracepointAttachInfo {
                    tracepoint: tracepoint.to_string(),
                }),
            )),
        }
    }
}

impl Commands {
    fn get_request_common(&self) -> anyhow::Result<Option<LoadRequestCommon>> {
        let id: &Option<String>;
        let section_name: &String;
        let global: &Option<Vec<GlobalArg>>;
        let command: &LoadCommands;
        let location: Option<load_request_common::Location>;

        let mut global_data: HashMap<String, Vec<u8>> = HashMap::new();

        match self {
            Commands::LoadFromFile(l) => {
                id = &l.id;
                section_name = &l.section_name;
                global = &l.global;
                command = &l.command;
                location = Some(load_request_common::Location::File(l.path.clone()));
            }
            Commands::LoadFromImage(l) => {
                id = &l.id;
                section_name = &l.section_name;
                global = &l.global;
                command = &l.command;
                location = {
                    let image_pull_policy: ImagePullPolicy = l
                        .image_pull_policy
                        .as_str()
                        .try_into()
                        .expect("invalid image pull policy");
                    let (username, password) = match l.registry_auth.clone() {
                        Some(a) => {
                            let auth_raw = general_purpose::STANDARD_NO_PAD.decode(a)?;

                            let auth_string = String::from_utf8(auth_raw)?;

                            let (username, password) = auth_string.split(':').next_tuple().unwrap();

                            (username.to_owned(), password.to_owned())
                        }
                        None => ("".to_owned(), "".to_owned()),
                    };

                    Some(load_request_common::Location::Image(BytecodeImage {
                        url: l.image_url.clone(),
                        image_pull_policy: image_pull_policy as i32,
                        username,
                        password,
                    }))
                };
            }
            _ => bail!("Unknown command"),
        };

        if let Some(global) = global {
            for g in global.iter() {
                global_data.insert(g.name.to_string(), g.value.clone());
            }
        }

        Ok(Some(LoadRequestCommon {
            id: id.clone(),
            location,
            section_name: section_name.to_string(),
            program_type: command.get_prog_type() as i32,
            global_data,
        }))
    }

    fn get_attach_info(&self) -> anyhow::Result<Option<AttachInfo>> {
        match self {
            Commands::LoadFromFile(l) => l.command.get_attach_type(),
            Commands::LoadFromImage(l) => l.command.get_attach_type(),
            _ => bail!("Unknown command"),
        }
    }
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
    // For output to bpfctl commands, eprintln() should be used. This includes
    // errors returned from bpfd. Every command should print some success indication
    // or a meaningful error.
    // logs (warn!(), info!(), debug!()) can be used by developers to help debug
    // failure cases. Being a CLI, they will be limited in their use. To see logs
    // for bpfctl commands, use the RUST_LOG environment variable:
    //    $ RUST_LOG=info bpfctl list
    env_logger::init();

    let cli = Cli::parse();

    let config = if let Ok(c) = fs::read_to_string(CFGPATH_BPFD_CONFIG) {
        c.parse().unwrap_or_else(|_| {
            warn!("Unable to parse config file, using defaults");
            Config::default()
        })
    } else {
        warn!("Unable to read config file, using defaults");
        Config::default()
    };

    let endpoint = &config.grpc.endpoint;
    // URI is ignored on UDS, so any parsable string works.
    let address = String::from("http://localhost");
    let unix = endpoint.unix.clone();
    match Endpoint::try_from(address)?
        .connect_with_connector(service_fn(move |_: Uri| UnixStream::connect(unix.clone())))
        .await
    {
        Ok(channel) => {
            info!("Using UNIX socket as transport");
            match execute_request(&cli.command, channel).await {
                Ok(_) => return Ok(()),
                Err(e) => {
                    eprintln!("Error = {e:?}");
                    return Ok(());
                }
            }
        }
        Err(e) => debug!("Error getting UNIX socket channel. Err: {}", e),
    }

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
    let address = SocketAddr::new(
        endpoint
            .address
            .parse()
            .unwrap_or_else(|_| panic!("failed to parse address '{}'", endpoint.address)),
        endpoint.port,
    );
    // TODO: Use https (https://github.com/bpfd-dev/bpfd/issues/396)
    let address = format!("http://{address}");
    let channel = Channel::from_shared(address)?
        .tls_config(tls_config)?
        .connect()
        .await?;

    info!("Using TLS over TCP socket as transport");
    if let Err(e) = execute_request(&cli.command, channel).await {
        eprintln!("Error = {e:?}")
    }

    Ok(())
}

async fn execute_request(command: &Commands, channel: Channel) -> anyhow::Result<()> {
    let mut client = LoaderClient::new(channel);
    match command {
        Commands::LoadFromFile(_) | Commands::LoadFromImage(_) => {
            let attach_info = match command.get_attach_info() {
                Ok(t) => t,
                Err(e) => bail!(e),
            };

            let common = match command.get_request_common() {
                Ok(t) => t,
                Err(e) => bail!(e),
            };

            let request = tonic::Request::new(LoadRequest {
                common,
                attach_info,
            });
            let response = client.load(request).await?.into_inner();
            println!("{}", response.id);
        }

        Commands::Unload { id } => {
            let request = tonic::Request::new(UnloadRequest { id: id.to_string() });
            let _response = client.unload(request).await?.into_inner();
        }
        Commands::List {} => {
            let request = tonic::Request::new(ListRequest { program_type: None });
            let response = client.list(request).await?.into_inner();
            let mut table = ProgTable::new();

            for r in response.results {
                if let Err(e) = table.add_response_prog(r) {
                    bail!(e)
                }
            }
            println!("{table}");
        }
    }
    Ok(())
}
