use std::sync::Arc;

use anyhow::{Context as anyhowContext, Result};
use bpfd_api::v1::loader_client::LoaderClient;
use bpfd_k8s_api::v1alpha1::EbpfProgram;
use futures::StreamExt;
use kube::{
    api::{Api, ListParams},
    runtime::controller::Controller,
    Client,
};
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Identity};
use tracing::*;

mod bpfd_agent;
mod finalizer;

// These are not configurable and must match the directories where
// they are mounted in the bpfd Daemonset.
const CA_CERT_PATH: &str = "/etc/bpfd/certs/ca/ca.crt";
const AGENT_CERT_PATH: &str = "/etc/bpfd/certs/bpfd-agent/tls.crt";
const AGENT_KEY_PATH: &str = "/etc/bpfd/certs/bpfd-agent/tls.key";

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();

    // Setup bpfd client
    let ca_cert = tokio::fs::read(CA_CERT_PATH)
        .await
        .context("CA Cert File does not exist")?;
    let ca_cert = Certificate::from_pem(ca_cert);
    let cert = tokio::fs::read(AGENT_CERT_PATH)
        .await
        .context("Cert File does not exist")?;
    let key = tokio::fs::read(AGENT_KEY_PATH)
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

    let bpfd_client = LoaderClient::new(channel);

    let client = Client::try_default().await.expect("create client");

    //TODO Add Metrics
    //let metrics = Metrics::new();
    //let diagnostics = Arc::new(RwLock::new(Diagnostics::new()));

    let context = Arc::new(bpfd_agent::Context {
        client: client.clone(),
        bpfd_client: bpfd_client.clone(),
        //metrics: metrics.clone(),
    });

    // Ensure the operator has installed CRD before starting controllers
    let ebpf_programs = Api::<EbpfProgram>::all(client);
    // Ensure CRD is installed before loop-watching
    let _r = ebpf_programs
        .list(&ListParams::default().limit(1))
        .await
        .expect("is the crd installed? please run: cargo run --bin crdgen | kubectl apply -f -");

    info!("starting bpfd-ebpf-program-controller");

    // Start controller and return its future.
    Controller::new(ebpf_programs, ListParams::default())
        .run(bpfd_agent::reconcile, bpfd_agent::error_policy, context)
        .for_each(|res| async move {
            match res {
                Ok(o) => info!("reconciled {:?}", o),
                Err(e) => error!("reconcile failed: {:?}", e),
            }
        })
        .await;

    info!("controller terminated");
    Ok(())
}
