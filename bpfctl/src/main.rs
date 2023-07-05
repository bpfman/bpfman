// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{collections::HashMap, fs, net::SocketAddr, str};

use anyhow::{bail, Context};
use base64::{engine::general_purpose, Engine as _};
use bpfd_api::{
    config::{self, Config},
    util::directories::*,
    v1::{
        list_response::{self, list_result::Location},
        load_request::{self, AttachInfo},
        load_request_common,
        loader_client::LoaderClient,
        BytecodeImage, ListRequest, LoadRequest, LoadRequestCommon, TcAttachInfo,
        TracepointAttachInfo, UnloadRequest, UprobeAttachInfo, XdpAttachInfo,
    },
    ImagePullPolicy, ProgramType, TcProceedOn, XdpProceedOn,
};
use clap::{Args, Parser, Subcommand};
use comfy_table::Table;
use hex::FromHex;
use itertools::Itertools;
use log::{info, warn};
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
    Unload(UnloadArgs),
    /// List all BPF programs loaded via bpfd.
    List(ListArgs),
    /// Get a program's metadata information specified by kernel id.
    Get { kernel_id: u32 },
}

#[derive(Args)]
struct ListArgs {
    /// Optional: Program type to list
    /// Example: --program-type xdp
    #[clap(short, long, verbatim_doc_comment)]
    program_type: Option<ProgramType>,

    // Optional: List all programs
    #[clap(short, long, verbatim_doc_comment)]
    all: bool,
}

#[derive(Args)]
struct LoadFileArgs {
    /// Required: Location of local bytecode file
    /// Example: --path /run/bpfd/examples/go-xdp-counter/bpf_bpfel.o
    #[clap(short, long, verbatim_doc_comment)]
    path: String,

    /// Required: Name of the ELF section from the object file.
    #[clap(short, long)]
    section_name: String,

    /// Optional: Program uuid to be used by bpfd. If not specified, bpfd will generate
    /// a uuid.
    #[clap(long, verbatim_doc_comment)]
    id: Option<String>,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

    #[clap(subcommand)]
    command: LoadCommands,
}

#[derive(Args)]
struct LoadImageArgs {
    /// Required: Container Image URL.
    /// Example: --image-url quay.io/bpfd-bytecode/xdp_pass:latest
    #[clap(short, long, verbatim_doc_comment)]
    image_url: String,

    /// Optional: Pull policy for remote images. Valid values: [Always, IfNotPresent, Never]
    #[clap(short, long, default_value = "IfNotPresent")]
    pull_policy: String,

    /// Optional: Registry auth for authenticating with the specified image registry.
    /// This should be base64 encoded from the '<username>:<password>' string just like
    /// it's stored in the docker/podman host config.
    /// Example: --registry_auth "YnjrcKw63PhDcQodiU9hYxQ2"
    #[clap(short, long, verbatim_doc_comment)]
    registry_auth: Option<String>,

    /// Optional: Name of the ELF section from the object file.
    #[clap(short, long, default_value = "")]
    section_name: String,

    /// Optional: Program uuid to be used by bpfd. If not specified, bpfd will generate
    /// a uuid.
    #[clap(long, verbatim_doc_comment)]
    id: Option<String>,

    /// Optional: Global variables to be set when program is loaded.
    /// Format: <NAME>=<Hex Value>
    ///
    /// This is a very low level primitive. The caller is responsible for formatting
    /// the byte string appropriately considering such things as size, endianness,
    /// alignment and packing of data structures.
    #[clap(short, long, verbatim_doc_comment, num_args(1..), value_parser=parse_global_arg)]
    global: Option<Vec<GlobalArg>>,

    #[clap(subcommand)]
    command: LoadCommands,
}

#[derive(Subcommand)]
enum LoadCommands {
    /// Install an eBPF program on the XDP hook point for a given interface.
    Xdp {
        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Valid values: [aborted, drop, pass, tx, redirect, dispatcher_return]
        /// Example: --proceed-on "pass" --proceed-on "drop"
        /// [default: pass, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    /// Install an eBPF program on the TC hook point for a given interface.
    Tc {
        /// Required: Direction to apply program. Valid values: [ingress, egress]
        #[clap(short, long)]
        direction: String,

        /// Required: Interface to load program on.
        #[clap(short, long)]
        iface: String,

        /// Required: Priority to run program in chain. Lower value runs first.
        #[clap(short, long)]
        priority: i32,

        /// Optional: Proceed to call other programs in chain on this exit code.
        /// Multiple values supported by repeating the parameter.
        /// Valid values: [unspec, ok, reclassify, shot, pipe, stolen, queued,
        /// repeat, redirect, trap, dispatcher_return]
        /// Example: --proceed-on "ok" --proceed-on "pipe"
        /// [default: ok, pipe, dispatcher_return]
        #[clap(long, verbatim_doc_comment, num_args(1..))]
        proceed_on: Vec<String>,
    },
    /// Install an eBPF program on a Tracepoint.
    Tracepoint {
        /// Required: The tracepoint to attach to.
        /// Example: --tracepoint "sched/sched_switch"
        #[clap(short, long, verbatim_doc_comment)]
        tracepoint: String,
    },
    /// Install an eBPF uprobe
    Uprobe {
        /// Optional: function to attach the uprobe to.
        #[clap(short, long)]
        fn_name: Option<String>,

        /// Optional: offset added to the address of the target function
        /// (or beginning of target if no function is identified)
        #[clap(short, long)]
        offset: Option<u64>,

        /// Required: library name or the absolute path to a binary or library
        /// Example: --target "libc".
        #[clap(short, long)]
        target: String,

        /// Optional: only execute uprobe for given process identification number (PID)
        /// If PID is not provided, uprobe executes for all PIDs.
        #[clap(short, long)]
        pid: Option<i32>,

        /// Optional: namespace to attach the uprobe in. (NOT CURRENTLY SUPPORTED)
        #[clap(short, long)]
        namespace: Option<String>,
    },
}

#[derive(Args)]
struct UnloadArgs {
    /// Required: Program uuid to be unloaded
    id: String,
}

#[derive(Clone, Debug)]
struct GlobalArg {
    name: String,
    value: Vec<u8>,
}

struct ProgTable(Table);

impl ProgTable {
    fn new() -> Self {
        let mut table = Table::new();

        table.load_preset(comfy_table::presets::NOTHING);
        table.set_header(vec!["Kernel_ID", "Name", "UUID", "Type", "Load_Time"]);
        ProgTable(table)
    }

    fn add_row(
        &mut self,
        name: String,
        uuid: String,
        kernel_id: String,
        type_: String,
        load_time: String,
    ) {
        self.0
            .add_row(vec![kernel_id, name, uuid, type_, load_time]);
    }

    fn add_response_prog(&mut self, r: list_response::ListResult) -> anyhow::Result<()> {
        self.add_row(
            r.name,
            r.id.unwrap_or("".to_string()),
            r.bpf_id.to_string(),
            (ProgramType::try_from(r.program_type)?).to_string(),
            r.loaded_at,
        );

        Ok(())
    }
}

impl std::fmt::Display for ProgTable {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

fn print_get(r: &list_response::ListResult) -> anyhow::Result<()> {
    // if program is managed by bpfd print UUID, Location and Metadata
    let bpfd_info = if let Some(uuid) = r.clone().id {
        let prog_type: ProgramType = r.program_type.try_into()?;
        let location = match &r.clone().location.unwrap() {
            // Cast imagePullPolicy into it's concrete type so we can easily print.
            Location::Image(i) => format!(
                r#"Image:                              {} 
ImagePullpolicy:                    {}"#,
                i.url,
                TryInto::<ImagePullPolicy>::try_into(i.image_pull_policy)?
            ),
            Location::File(p) => format!(r#"Path:                           {p}"#),
            _ => "".to_owned(),
        };

        let metadata = match prog_type {
            ProgramType::Xdp => {
                if let Some(list_response::list_result::AttachInfo::XdpAttachInfo(
                    XdpAttachInfo {
                        priority,
                        iface,
                        position,
                        proceed_on,
                    },
                )) = r.clone().attach_info
                {
                    let proc_on = match XdpProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };
                    format!(
                        r#"Priority:                           {priority}
Iface:                              {iface}
Position:                           {position}
Proceed_on:                         {proc_on} "#
                    )
                } else {
                    "".to_string()
                }
            }
            ProgramType::Tc => {
                if let Some(list_response::list_result::AttachInfo::TcAttachInfo(TcAttachInfo {
                    priority,
                    iface,
                    position,
                    direction,
                    proceed_on,
                })) = r.clone().attach_info
                {
                    let proc_on = match TcProceedOn::from_int32s(proceed_on) {
                        Ok(p) => p,
                        Err(e) => bail!("error parsing proceed_on {e}"),
                    };

                    format!(
                        r#"Priority:                           {priority}
Iface:                              {iface}
Position:                           {position}
Direction:                          {direction}
Proceed_on:                         {proc_on}"#
                    )
                } else {
                    "".to_string()
                }
            }
            ProgramType::Tracepoint => {
                if let Some(list_response::list_result::AttachInfo::TracepointAttachInfo(
                    TracepointAttachInfo { tracepoint },
                )) = r.clone().attach_info
                {
                    format!(
                        r#"
Tracepoint:                         {tracepoint}"#
                    )
                } else {
                    "".to_string()
                }
            }
            ProgramType::Kprobe => {
                if let Some(list_response::list_result::AttachInfo::UprobeAttachInfo(
                    UprobeAttachInfo {
                        fn_name,
                        offset,
                        target,
                        pid,
                        namespace,
                    },
                )) = r.clone().attach_info
                {
                    let fn_name = fn_name.unwrap_or("None".to_string());

                    let offset = match offset {
                        Some(o) => o.to_string(),
                        None => "None".to_string(),
                    };
                    let pid = match pid {
                        Some(p) => p.to_string(),
                        None => "None".to_string(),
                    };
                    let namespace = namespace.unwrap_or("None".to_string());

                    format!(
                        r#"fn_name:                            {fn_name}
Offset:                             {offset}
Target:                             {target}
Pid:                                {pid}
Namespace:                          {namespace}"#
                    )
                } else {
                    "".to_string()
                }
            }
            // skip unknown program types
            _ => {
                bail!("program has bpfd UUID but no attach info")
            }
        };
        format!(
            r#"
UUID:                               {}                              
{}             
{}"#,
            uuid, location, metadata
        )
    } else {
        "NONE".to_string()
    };

    let global_info = format!(
        r#"
Kernel_ID:                          {}
Name:                               {}
Type:                               {}
Loaded At:                          {}
Tag:                                {}
GPL Compatible:                     {}
Map IDs:                            {:?}
BTF ID:                             {}
Size Translated (bytes):            {}
JITed:                              {}
Size JITed (bytes):                 {}
Kernel Allocated Memory (bytes):    {}
Verified Instruction Count:         {}
"#,
        r.bpf_id,
        r.name,
        ProgramType::try_from(r.program_type)?,
        r.loaded_at,
        r.tag,
        r.gpl_compatible,
        r.map_ids,
        r.btf_id,
        r.bytes_xlated,
        r.jited,
        r.bytes_jited,
        r.bytes_memlock,
        r.verified_insns
    );
    println!();
    println!("#################### Bpfd State ####################");
    println!("{}", bpfd_info);
    println!();
    println!("#################### Kernel State ##################");
    println!("{}", global_info);

    Ok(())
}

impl LoadCommands {
    fn get_prog_type(&self) -> ProgramType {
        match self {
            LoadCommands::Xdp { .. } => ProgramType::Xdp,
            LoadCommands::Tc { .. } => ProgramType::Tc,
            LoadCommands::Tracepoint { .. } => ProgramType::Tracepoint,
            LoadCommands::Uprobe { .. } => ProgramType::Kprobe,
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
            LoadCommands::Uprobe {
                fn_name,
                offset,
                target,
                pid,
                namespace,
            } => {
                if namespace.is_some() {
                    bail!("uprobe namespace option not supported yet");
                }
                Ok(Some(load_request::AttachInfo::UprobeAttachInfo(
                    UprobeAttachInfo {
                        fn_name: fn_name.clone(),
                        offset: *offset,
                        target: target.to_string(),
                        pid: *pid,
                        namespace: namespace.clone(),
                    },
                )))
            }
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
                    let pull_policy: ImagePullPolicy = l
                        .pull_policy
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
                        image_pull_policy: pull_policy as i32,
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
            program_type: command.get_prog_type() as u32,
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

    for endpoint in config.grpc.endpoints {
        match endpoint {
            config::Endpoint::Tcp {
                address,
                port,
                enabled,
            } if !enabled => info!("Skipping disabled endpoint on {address}, port: {port}"),
            config::Endpoint::Tcp {
                address,
                port,
                enabled: _,
            } => match execute_request_tcp(&cli.command, address, port, tls_config.clone()).await {
                Ok(_) => return Ok(()),
                Err(e) => eprintln!("Error = {e:?}"),
            },
            config::Endpoint::Unix { path, enabled } if !enabled => {
                info!("Skipping disabled endpoint on {path}")
            }
            config::Endpoint::Unix { path, enabled: _ } => {
                match execute_request_unix(&cli.command, path).await {
                    Ok(_) => return Ok(()),
                    Err(e) => eprintln!("Error = {e:?}"),
                }
            }
        }
    }
    bail!("Failed to execute request")
}

async fn execute_request_unix(command: &Commands, path: String) -> anyhow::Result<()> {
    // URI is ignored on UDS, so any parsable string works.
    let address = String::from("http://localhost");
    let channel = Endpoint::try_from(address)?
        .connect_with_connector(service_fn(move |_: Uri| UnixStream::connect(path.clone())))
        .await?;

    info!("Using UNIX socket as transport");
    execute_request(command, channel).await
}

async fn execute_request_tcp(
    command: &Commands,
    address: String,
    port: u16,
    tls_config: ClientTlsConfig,
) -> anyhow::Result<()> {
    let address = SocketAddr::new(
        address
            .parse()
            .unwrap_or_else(|_| panic!("failed to parse address '{}'", address)),
        port,
    );

    // TODO: Use https (https://github.com/bpfd-dev/bpfd/issues/396)
    let address = format!("http://{address}");
    let channel = Channel::from_shared(address)?
        .tls_config(tls_config)?
        .connect()
        .await?;

    info!("Using TLS over TCP socket as transport");
    execute_request(command, channel).await
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

        Commands::Unload(l) => {
            let request = tonic::Request::new(UnloadRequest {
                id: l.id.to_string(),
            });
            let _response = client.unload(request).await?.into_inner();
        }
        Commands::List(l) => {
            let prog_type_filter = l.program_type.map(|p| p as u32);

            let request = tonic::Request::new(ListRequest {
                program_type: prog_type_filter,
                bpfd_programs_only: Some(!l.all),
            });
            let response = client.list(request).await?.into_inner();
            let mut table = ProgTable::new();

            for r in response.results {
                if let Err(e) = table.add_response_prog(r) {
                    bail!(e)
                }
            }
            println!("{table}");
        }
        Commands::Get { kernel_id } => {
            let request = tonic::Request::new(ListRequest {
                program_type: None,
                bpfd_programs_only: None,
            });
            let response = client.list(request).await?.into_inner();

            let prog = response
                .results
                .iter()
                .find(|r| r.bpf_id == *kernel_id)
                .unwrap_or_else(|| panic!("No program with kernel ID {}", kernel_id));

            if let Err(e) = print_get(prog) {
                bail!(e)
            }
        }
    }
    Ok(())
}
