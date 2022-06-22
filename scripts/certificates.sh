#!/bin/sh
set -e

KEY_LEN=4096
CERT_PATH=/etc/bpfd/certs

# generate ca cert
mkdir -p "${CERT_PATH}"/ca
mkdir -p "${CERT_PATH}"/bpfd
mkdir -p "${CERT_PATH}"/bpfctl

if [ ! -f "${CERT_PATH}"/ca/ca.pem ]; then
    openssl genrsa -out "${CERT_PATH}"/ca/ca.key ${KEY_LEN}
    openssl req -new -x509 -key "${CERT_PATH}"/ca/ca.key -subj "/CN=bpfd-ca/" -out "${CERT_PATH}"/ca/ca.pem
    chmod -v 0400 "${CERT_PATH}"/ca/ca.pem "${CERT_PATH}"/ca/ca.key
fi

if [ ! -f "${CERT_PATH}"/bpfd/bpfd.pem ]; then
    openssl genrsa -out "${CERT_PATH}"/bpfd/bpfd.key ${KEY_LEN}
    openssl req -new -key "${CERT_PATH}"/bpfd/bpfd.key \
        -subj "/CN=bpfd/" \
        -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
        -out "${CERT_PATH}"/bpfd/bpfd.csr
    openssl x509 -req -in "${CERT_PATH}"/bpfd/bpfd.csr \
        -CA "${CERT_PATH}"/ca/ca.pem \
        -CAkey "${CERT_PATH}"/ca/ca.key \
        -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
        -out "${CERT_PATH}"/bpfd/bpfd.pem
    rm "${CERT_PATH}"/bpfd/bpfd.csr
    chmod -v 0400 "${CERT_PATH}"/bpfd/bpfd.pem "${CERT_PATH}"/bpfd/bpfd.key
fi

if [ ! -f "${CERT_PATH}"/bpfctl/bpfctl.pem ]; then
    openssl genrsa -out "${CERT_PATH}"/bpfctl/bpfctl.key ${KEY_LEN}
    openssl req -new -key "${CERT_PATH}"/bpfctl/bpfctl.key \
        -subj "/CN=bpfctl/" \
        -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
        -out "${CERT_PATH}"/bpfctl/bpfctl.csr
    openssl x509 -req -in "${CERT_PATH}"/bpfctl/bpfctl.csr \
        -CA "${CERT_PATH}"/ca/ca.pem \
        -CAkey "${CERT_PATH}"/ca/ca.key \
        -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
        -out "${CERT_PATH}"/bpfctl/bpfctl.pem
    rm "${CERT_PATH}"/bpfctl/bpfctl.csr
    chmod -v 0400 "${CERT_PATH}"/bpfctl/bpfctl.pem "${CERT_PATH}"/bpfctl/bpfctl.key
fi
