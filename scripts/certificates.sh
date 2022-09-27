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
    mkdir -p "${CA_CERT_PATH}"
    if [ ! -f "${CA_CERT_PATH}"/ca.pem ] || [ "${regen}" == true ]; then
        openssl genrsa -out "${CA_CERT_PATH}"/ca.key ${KEY_LEN}
        openssl req -new -x509 -key "${CA_CERT_PATH}"/ca.key -subj "/CN=bpfd-ca/" -out "${CA_CERT_PATH}"/ca.pem
        # Set the private key such that only members of the "bpfd" group can read
        chmod -v 0440 "${CA_CERT_PATH}"/ca.key
        # Set the public key such that any user can read
        chmod -v 0444 "${CA_CERT_PATH}"/ca.pem
    fi

    cert_client "${BIN_BPFD}" "${USER_BPFD}" ${regen}
    cert_client "${BIN_BPFCTL}" "${USER_BPFCTL}" ${regen}
    if [ "${regen}" == true ]; then
        cert_client "${BIN_GOCOUNTER}" "${USER_BPFCTL}" ${regen}
    fi
}

cert_client() {
    sub_directory=$1
    user_name=$2
    regen=$3
    if [ -z "${sub_directory}" ]; then
        echo "Sub-directory name required"
        exit 1
    fi
    if [ -z "${user_name}" ]; then
        echo "Client name required"
        exit 1
    fi
    if [ -z "${regen}" ]; then
        regen=false
    fi

    BASE_PATH="/etc/${user_name}"
    CERT_PATH="${BASE_PATH}/certs/${sub_directory}"

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
            -CA "${CA_CERT_PATH}/ca.pem" \
            -CAkey "${CA_CERT_PATH}/ca.key" \
            -CAcreateserial \
            -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1") \
            -out "${CERT_PATH}/${sub_directory}.pem"
        rm "${CERT_PATH}/${sub_directory}.csr"
        # Set the private and public keys such that only members of the user group can read
        chmod -v 0440 "${CERT_PATH}/${sub_directory}.pem" "${CERT_PATH}/${sub_directory}.key"
    fi
}
