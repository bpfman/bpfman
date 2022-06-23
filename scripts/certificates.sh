#!/bin/sh
set -e

KEY_LEN=4096
CERT_PATH=/etc/bpfd/certs

init() {
    # generate ca cert
    mkdir -p "${CERT_PATH}"/ca
    if [ ! -f "${CERT_PATH}"/ca/ca.pem ]; then
        openssl genrsa -out "${CERT_PATH}"/ca/ca.key ${KEY_LEN}
        openssl req -new -x509 -key "${CERT_PATH}"/ca/ca.key -subj "/CN=bpfd-ca/" -out "${CERT_PATH}"/ca/ca.pem
        chmod -v 0400 "${CERT_PATH}"/ca/ca.pem "${CERT_PATH}"/ca/ca.key
    fi
    client bpfd
    client bpfctl
}

client() {
    name=$1
    if [ -z "${name}" ]; then
        echo "Client name required"
        exit 1
    fi
    mkdir -p "${CERT_PATH}/${name}"
    if [ ! -f "${CERT_PATH}/${name}/${name}.pem" ]; then
        openssl genrsa -out "${CERT_PATH}/${name}/${name}.key" ${KEY_LEN}
        openssl req -new -key "${CERT_PATH}/${name}/${name}.key" \
            -subj "/CN=${name}/" \
            -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
            -out "${CERT_PATH}/${name}/${name}.csr"
        openssl x509 -req -in "${CERT_PATH}/${name}/${name}.csr" \
            -CA "${CERT_PATH}/ca/ca.pem" \
            -CAkey "${CERT_PATH}/ca/ca.key" \
            -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
            -out "${CERT_PATH}/${name}/${name}.pem"
        rm "${CERT_PATH}/${name}/${name}.csr"
        chmod -v 0400 "${CERT_PATH}/${name}/${name}.pem" "${CERT_PATH}/${name}/${name}.key"
    fi
}

case $1 in
    "init")
        init
        ;;
    "client")
        client "$2"
        ;;
    *)
        echo "command required. init or client <name>"
        exit 1
esac
