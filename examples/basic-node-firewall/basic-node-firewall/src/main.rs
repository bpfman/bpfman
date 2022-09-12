use aya::maps::{HashMap, perf::AsyncPerfEventArray, Map};
use std::{net::{self, Ipv4Addr}, convert::TryInto, thread, time};
use clap::Parser;
use std::path::PathBuf;
use aya::util::online_cpus;
use bytes::BytesMut;
use tokio::{task, signal};
use k8s_openapi::api::core::v1::ConfigMap;
use kube::{
    api::{Api},
    Client,
};
use bpfd_k8s_api::v1alpha1::EbpfProgram;
use tracing::*;

extern crate pnet;
use pnet::datalink;

use basic_node_firewall_common::{packet_five_tuple, packet_log};

#[derive(Debug, Parser)]
struct Opt {
    #[clap(short, long, default_value = "eth0")]
    iface: String,
}

fn map_to_protocol(proto: &str) -> u8 {
    match proto { 
        "TCP" => 6,
        _ => {
            error!("Protocol not supported Defaulting to TCP");
             6
        },
    }
}

fn map_from_protocol(proto: u8) -> String {
    match proto { 
        6 => "TCP".to_owned(),
        _ => {
            error!("Protocol not supported Defaulting to TCP");
            "TCP".to_owned()
        },
    }
}

#[tokio::main]
async fn main() -> Result<(), anyhow::Error> {
    //env_logger::init();
    tracing_subscriber::fmt::init();
    let client = Client::try_default().await?;
    let cm_api: Api<ConfigMap> = Api::default_namespaced(client.clone());
    let bpf_prog_api: Api<EbpfProgram> = Api::default_namespaced(client.clone());
    let mut prog_id: &'static str = "";
    let mut prog_interface = "".to_owned();
    let mut blocklist:Vec<packet_five_tuple> = Vec::new(); 
    
    // Ugly preprocessing of all data we need
    info!("Starting basic-node-firewall manager");
 
    while prog_id == "" && prog_interface == "" && blocklist.len() == 0 { 
        thread::sleep(time::Duration::from_secs(2));
        // Verify we can get it

        if let Some(epcpy) = bpf_prog_api.get_opt("basic-node-firewall").await? {
            info!("Got Ebpf basic-node-firewall program");
            let annotations = match epcpy.metadata.annotations {
                Some(tmp) => tmp,
                None => { 
                    debug!("EbpfProgram basic-node-firewall not programed yet");
                    continue;
                }
            };

            // Make this static so we can use it later in a spawned thread used to watch events
            prog_id = Box::leak(annotations.get("bpfd.ebpfprogram.io/uuid").unwrap().to_string().into_boxed_str());
            prog_interface = annotations.get("bpfd.ebpfprogram.io/attach_point").unwrap().to_string();
            info!("Program interface: {} id: {}", prog_interface, prog_id);

        } else {
            debug!("Failed to get EbpfProgram basic-node-firewall");
            continue
        }; 

        let all_interfaces = datalink::interfaces();

        debug!("Interface dump {:?}",all_interfaces);
        // Search for the default interface - the one that is
        // up, not loopback and has an IP.
        let default_interface = all_interfaces
            .iter()
            .find(|e| e.is_up() && !e.is_loopback() && !e.ips.is_empty() && e.name.eq(&prog_interface));
        
        match default_interface {
            Some(interface) => info!("Found default interface with [{}] and IPs [{:?}].", 
            interface.name, interface.ips),
            None => {
                error!("Error while finding the default interface.");
                continue
            }    
        }

        let int_v4_ip = default_interface.unwrap().ips
            .iter()
            .find(|e| e.is_ipv4()).unwrap().ip();
        
        // Ugly conversion to net::Ipv4Addr so we can cast to u32
        let ip4 = int_v4_ip.to_string().parse::<Ipv4Addr>().unwrap();
        

        info!("Get Ebpf basic_node_firewall configmap");
        match cm_api.get_opt("basic-node-firewall-config").await? { 
            Some(cmcpy) => {
                debug!("Extracting blocklist from config Object");
                if let Some(data) = &cmcpy.data{
                    if data.contains_key("blocklist") {
                        for entry in &mut data["blocklist"].lines(){
                            let port_proto:Vec<&str> = entry.split(":").collect();
                            blocklist.push(packet_five_tuple {
                                src_address: 0, 
                                dst_address: ip4.try_into()?,
                                src_port: 0, 
                                dst_port: port_proto[0].parse().unwrap(), 
                                protocol: map_to_protocol(port_proto[1]),
                                _pad: [0, 0, 0],
                            })
                        } 
                    } else { 
                        continue;
                    }
                } else { 
                    continue;
                }
            },
            None => {
                debug!("blocklist configmap does not exist");
                continue
            }
        };
    }

    // Load Deny rules into blocklist
    debug!("blocklist path: {}",format!("/var/run/bpfd/fs/maps/{}/blocklist", prog_id));
    let block_map_pin_path = PathBuf::from(format!("/var/run/bpfd/fs/maps/{}/blocklist", prog_id).as_str());

    let mut block_map = Map::from_pin(block_map_pin_path)?; 

    let mut blocklist_map: HashMap<_, packet_five_tuple, u32> =
    HashMap::try_from(&mut block_map)?;

    // Delete any existing keys in blocklist before writing new ones
    let existing_keys = blocklist_map.keys().collect::<Result<Vec<_>, _>>()?;
    existing_keys.iter().try_for_each(|key| -> Result<(), aya::maps::MapError> {
            blocklist_map.remove(&key)
    })?;

    // Write new blocklist entries
    blocklist.iter().try_for_each( |tuple| -> Result<(), aya::maps::MapError> {
        blocklist_map.insert(*tuple, 0, 0)
    })?;

    // Query / dump events
    for cpu_id in online_cpus()? {
        task::spawn(async move { 
            let events_map_pin_path = PathBuf::from(format!("/var/run/bpfd/fs/maps/{}/events", &prog_id).as_str());

            let mut events_map = Map::from_pin(events_map_pin_path).unwrap(); 
        
            // Loop forever emitting any events
            let mut perf_array: AsyncPerfEventArray<_> =
            AsyncPerfEventArray::try_from(&mut events_map).unwrap();

            let mut buf = perf_array.open(cpu_id, None).unwrap();

            let mut buffers = (0..10)
                .map(|_| BytesMut::with_capacity(1024))
                .collect::<Vec<_>>();

            loop {
                let events = buf.read_events(&mut buffers).await.unwrap();
                for i in 0..events.read {
                    let buf = &mut buffers[i];
                    let ptr = buf.as_ptr() as *const packet_log;
                    let data = unsafe { ptr.read_unaligned() };
                    let src_addr = net::Ipv4Addr::from(data.src_address);
                    let dst_addr = net::Ipv4Addr::from(data.dst_address);
                    info!("MY BASIC-NODE-FIREWALL DROPPED A PACKET! ---->
                    SRC_IP {},
                    DST_IP {},
                    SRC_PORT {},
                    DST_PORT {},
                    PROTOCOL {}", src_addr,dst_addr, data.src_port, data.dst_port, map_from_protocol(data.protocol));
                }
            }
        });
    }
    signal::ctrl_c().await.expect("failed to listen for event");
    Ok::<_, anyhow::Error>(())
}
