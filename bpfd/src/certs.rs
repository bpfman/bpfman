// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

use std::{fs, os::unix::fs::PermissionsExt, path::Path, str};

use bpfd_api::config::TlsConfig;
use log::{debug, error, info};
use openssl::{
    asn1::Asn1Time,
    bn::{BigNum, MsbOption},
    hash::MessageDigest,
    pkey::PKey,
    rsa::Rsa,
    x509::{
        extension::{
            AuthorityKeyIdentifier, BasicConstraints, SubjectAlternativeName, SubjectKeyIdentifier,
        },
        X509NameBuilder, X509ReqBuilder, X509,
    },
};
use thiserror::Error;
use tonic::transport::{Certificate, Identity};

#[derive(Error, Debug)]
pub enum CertsError {
    #[error("An error occurred. {0}")]
    Error(String),
    #[error(transparent)]
    CertOpensslError(#[from] openssl::error::ErrorStack),
}

const KEY_LENGTH: u32 = 4096;
const CERT_EXP_DAYS: u32 = 30;
const CA_CN_NAME: &str = "bpfd-ca";
const CN_BPFD_NAME: &str = "bpfd";
const CN_BPFD_CLIENT_NAME: &str = "bpfd-client";
const CA_KEY_FILENAME: &str = "ca.key";

pub async fn get_tls_config(tls: &TlsConfig) -> Result<(Certificate, Identity), CertsError> {
    // Read CA Cert
    let ca_cert_pem = match tokio::fs::read(&tls.ca_cert).await {
        Ok(ca_cert_pem) => {
            debug!("CA Certificate file {} exists.", tls.ca_cert);
            ca_cert_pem
        }
        Err(_) => {
            // CA Cert does not exist, so create a CA Certificate
            info!(
                "CA Certificate file {} does not exist. Creating CA Certificate.",
                tls.ca_cert
            );
            generate_ca_cert_pem(&tls.ca_cert).await?
        }
    };

    // Read bpfd Cert Key and Cert files and create an identity
    let identity = match tokio::fs::read(&tls.key).await {
        Ok(key) => {
            debug!("Certificate Key {} exists.", tls.key);

            // If Key exists but Cert doesn't, return error
            let cert_pem = tokio::fs::read(&tls.cert)
                .await
                .map_err(|_| CertsError::Error("certificate file does not exist".to_string()))?;

            Identity::from_pem(cert_pem, key)
        }
        Err(_) => {
            // Cert Key does not exist, so create a bpfd Certificate
            info!(
                "bpfd Certificate Key {} does not exist. Creating bpfd Certificate.",
                tls.key
            );
            let (cert_pem, cert_key_pem) = generate_cert(
                &tls.ca_cert,
                &ca_cert_pem,
                &tls.key,
                &tls.cert,
                CN_BPFD_NAME,
            )
            .await?;

            Identity::from_pem(cert_pem, cert_key_pem)
        }
    };

    // Read bpfd-client Cert Key and Cert files and make sure they exist. If they don't, create them.
    match tokio::fs::read(&tls.client_key).await {
        Ok(_) => {
            debug!("bpfd-client Certificate Key {} exists.", tls.client_key);

            // If Key exists but Cert doesn't, return error
            let _ = tokio::fs::read(&tls.client_cert).await.map_err(|_| {
                CertsError::Error("bpfd-client certificate file does not exist".to_string())
            })?;
        }
        Err(_) => {
            // Cert Key does not exist, so create a bpfd Certificate
            info!(
                "bpfd-client Certificate Key {} does not exist. Creating bpfd-client Certificate.",
                tls.client_key
            );
            if (generate_cert(
                &tls.ca_cert,
                &ca_cert_pem,
                &tls.client_key,
                &tls.client_cert,
                CN_BPFD_CLIENT_NAME,
            )
            .await)
                .is_err()
            {
                info!("Unable to create bpfd-client Certificate. Continuing");
            }
        }
    };

    Ok((Certificate::from_pem(ca_cert_pem), identity))
}

async fn generate_ca_cert_pem(ca_cert_path: &str) -> Result<Vec<u8>, CertsError> {
    // Generate the Private Key and write to a file.
    let rsa = Rsa::generate(KEY_LENGTH)?;
    let ca_cert_key = PKey::from_rsa(rsa)?;

    // Determine CA Key filename based on input CA Cert filename and write
    let path = Path::new(ca_cert_path);
    let ca_dir = path.parent().unwrap();
    let ca_key_path = ca_dir.join(CA_KEY_FILENAME);
    let ca_key_path = ca_key_path.to_str().unwrap();
    tokio::fs::write(ca_key_path, ca_cert_key.private_key_to_pem_pkcs8().unwrap())
        .await
        .map_err(|_| CertsError::Error("unable to write ca key to file".to_string()))?;
    // Set the private key such that only members of the "bpfd" group can read
    tokio::fs::set_permissions(ca_key_path, fs::Permissions::from_mode(0o0440))
        .await
        .map_err(|_| CertsError::Error("unable to set ca key file permissions".to_string()))?;

    // Generate the CA Certificate and write to a file.
    let mut x509_name = X509NameBuilder::new()?;
    x509_name.append_entry_by_text("CN", CA_CN_NAME)?;
    let x509_name = x509_name.build();

    let mut cert_builder = X509::builder()?;
    cert_builder.set_version(2)?;
    let serial_number = {
        let mut serial = BigNum::new()?;
        serial.rand(159, MsbOption::MAYBE_ZERO, false)?;
        serial.to_asn1_integer()?
    };
    cert_builder.set_serial_number(&serial_number)?;
    cert_builder.set_subject_name(&x509_name)?;
    cert_builder.set_issuer_name(&x509_name)?;
    cert_builder.set_pubkey(&ca_cert_key)?;
    let not_before = Asn1Time::days_from_now(0)?;
    cert_builder.set_not_before(&not_before)?;
    let not_after = Asn1Time::days_from_now(CERT_EXP_DAYS)?;
    cert_builder.set_not_after(&not_after)?;

    let subject_key_identifier =
        SubjectKeyIdentifier::new().build(&cert_builder.x509v3_context(None, None))?;
    cert_builder.append_extension(subject_key_identifier)?;

    let auth_key_identifier = AuthorityKeyIdentifier::new()
        .keyid(false)
        .issuer(false)
        .build(&cert_builder.x509v3_context(None, None))?;
    cert_builder.append_extension(auth_key_identifier)?;

    cert_builder.append_extension(BasicConstraints::new().critical().ca().build()?)?;

    cert_builder.sign(&ca_cert_key, MessageDigest::sha256())?;
    let ca_cert = cert_builder.build();

    let ca_cert_pem = ca_cert.to_pem().map_err(CertsError::CertOpensslError)?;

    tokio::fs::write(&ca_cert_path, ca_cert_pem.clone())
        .await
        .map_err(|_| CertsError::Error("unable to ca pem to file".to_string()))?;

    Ok(ca_cert_pem)
}

async fn generate_cert(
    ca_cert_path: &str,
    ca_cert_pem: &[u8],
    cert_key_path: &str,
    cert_path: &str,
    cert_cn_name: &str,
) -> Result<(Vec<u8>, Vec<u8>), CertsError> {
    // Determine CA Key filename based on input CA Cert filename and read
    let path = Path::new(ca_cert_path);
    let ca_dir = path.parent().unwrap();
    let ca_key_path = ca_dir.join(CA_KEY_FILENAME);
    let ca_key_path = ca_key_path.to_str().unwrap();
    let ca_key_pem = tokio::fs::read(&ca_key_path)
        .await
        .map_err(|_| CertsError::Error("ca certificate key does not exist".to_string()))?;
    let ca_key = PKey::private_key_from_pem(&ca_key_pem)?;

    // Generate the Private Key and write to a file.
    let rsa = Rsa::generate(KEY_LENGTH)?;
    let cert_key = PKey::from_rsa(rsa)?;
    let cert_key_pem = cert_key.private_key_to_pem_pkcs8()?;

    tokio::fs::write(cert_key_path, &cert_key_pem)
        .await
        .map_err(|_| CertsError::Error("unable to write key to file".to_string()))?;

    // Generate the Certificate and write to a file.
    let ca_cert_x590 = X509::from_pem(ca_cert_pem)?;

    let mut req_builder = X509ReqBuilder::new()?;
    req_builder.set_pubkey(&cert_key)?;

    let mut x509_name = X509NameBuilder::new()?;
    x509_name.append_entry_by_text("CN", cert_cn_name)?;
    let x509_name = x509_name.build();
    req_builder.set_subject_name(&x509_name)?;

    req_builder.sign(&cert_key, MessageDigest::sha256())?;
    let req = req_builder.build();

    let mut cert_builder = X509::builder()?;
    cert_builder.set_version(2)?;
    let serial_number = {
        let mut serial = BigNum::new()?;
        serial.rand(159, MsbOption::MAYBE_ZERO, false)?;
        serial.to_asn1_integer()?
    };
    cert_builder.set_serial_number(&serial_number)?;
    cert_builder.set_subject_name(req.subject_name())?;
    cert_builder.set_issuer_name(ca_cert_x590.subject_name())?;
    cert_builder.set_pubkey(&cert_key)?;
    let not_before = Asn1Time::days_from_now(0)?;
    cert_builder.set_not_before(&not_before)?;
    let not_after = Asn1Time::days_from_now(CERT_EXP_DAYS)?;
    cert_builder.set_not_after(&not_after)?;

    let subject_alt_name = SubjectAlternativeName::new()
        .dns("localhost")
        .ip("127.0.0.1")
        .ip("::1")
        .build(&cert_builder.x509v3_context(Some(&ca_cert_x590), None))?;
    cert_builder.append_extension(subject_alt_name)?;

    let subject_key_identifier = SubjectKeyIdentifier::new()
        .build(&cert_builder.x509v3_context(Some(&ca_cert_x590), None))?;
    cert_builder.append_extension(subject_key_identifier)?;

    let auth_key_identifier = AuthorityKeyIdentifier::new()
        .keyid(false)
        .issuer(false)
        .build(&cert_builder.x509v3_context(Some(&ca_cert_x590), None))?;
    cert_builder.append_extension(auth_key_identifier)?;

    // Self Sign
    cert_builder.sign(&ca_key, MessageDigest::sha256())?;
    let cert = cert_builder.build();
    let cert_pem = cert.to_pem().map_err(CertsError::CertOpensslError)?;

    tokio::fs::write(&cert_path, cert_pem.clone())
        .await
        .map_err(|_| CertsError::Error("unable to write certificate pem to file".to_string()))?;

    Ok((cert_pem, cert_key_pem))
}
