#!/bin/bash

KEY_LEN=4096

cert_init() {
    regen=$1
    if [ -z "${regen}" ]; then
        regen=false
    fi

    if [ "${regen}" == false ]; then
        echo "Creating certs:"
    else
        echo "Regenerating certs:"
    fi

    # generate ca cert
    mkdir -p "${CFG_CA_CERT_DIR}"
    if [ ! -f "${CFG_CA_CERT_DIR}"/ca.pem ] || [ "${regen}" == true ]; then
        openssl genrsa -out "${CFG_CA_CERT_DIR}"/ca.key ${KEY_LEN}
        openssl req -new -x509 -key "${CFG_CA_CERT_DIR}"/ca.key -subj "/CN=bpfd-ca/" -out "${CFG_CA_CERT_DIR}"/ca.pem
        # Set the private key such that only members of the "bpfd" group can read
        chmod -v 0440 "${CFG_CA_CERT_DIR}"/ca.key
        # Set the public key such that any user can read
        chmod -v 0444 "${CFG_CA_CERT_DIR}"/ca.pem
    fi

    cert_client "${BIN_BPFD}" ${regen}
    cert_client "${BIN_BPFD_CLIENT}" ${regen}
}

cert_client() {
    sub_directory=$1
    regen=$2
    if [ -z "${sub_directory}" ]; then
        echo "Sub-directory name required"
        exit 1
    fi
    if [ -z "${regen}" ]; then
        regen=false
    fi

    CERT_PATH="${CONFIGURATION_DIR}/certs/${sub_directory}"

    # If $regen is true, only regenerate certs that already existed.
    if [ ! -f "${CERT_PATH}/${sub_directory}.pem" ] && [ $regen == true ]; then
        exit 0
    fi

    mkdir -p "${CERT_PATH}"
    if [ ! -f "${CERT_PATH}/${sub_directory}.pem" ] || [ $regen == true ]; then
        openssl genrsa -out "${CERT_PATH}/${sub_directory}.key" ${KEY_LEN}
        openssl req -new -key "${CERT_PATH}/${sub_directory}.key" \
            -subj "/CN=${sub_directory}/" \
            -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
            -out "${CERT_PATH}/${sub_directory}.csr"
        openssl x509 -req -in "${CERT_PATH}/${sub_directory}.csr" \
            -CA "${CFG_CA_CERT_DIR}/ca.pem" \
            -CAkey "${CFG_CA_CERT_DIR}/ca.key" \
            -CAcreateserial \
            -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
            -out "${CERT_PATH}/${sub_directory}.pem"
        rm "${CERT_PATH}/${sub_directory}.csr"
        # Set the private and public keys such that only members of the user group can read
        chmod -v 0440 "${CERT_PATH}/${sub_directory}.pem" "${CERT_PATH}/${sub_directory}.key"
    fi
}
