#!/bin/bash

delete_directories() {
    # Remove directories
    if test -d "${CONFIGURATION_DIR}"; then
        echo "Deleting \"bpfman\" specific directories"
        echo "  Deleting \"${CONFIGURATION_DIR}\""
        rm -rf "${CONFIGURATION_DIR}"
    fi
    if test -d "${RUNTIME_DIR}"; then
        echo "  Deleting \"${RUNTIME_DIR}\""
        umount "${RTDIR_FS}"
        rm -rf "${RUNTIME_DIR}"
    fi
    if test -d "${RUNTIME_SOCKET_DIR}"; then
        echo "  Deleting \"${RUNTIME_SOCKET_DIR}\""
        rm -rf "${RUNTIME_SOCKET_DIR}"
    fi
    if test -d "${STATE_DIR}"; then
        echo "  Deleting \"${STATE_DIR}\""
        rm -rf "${STATE_DIR}"
    fi
}

# TO BE REMOVED!
# Left around to cleanup deprecated `bpfd` directories
delete_bpfd_directories() {
    BPFD_CONFIGURATION_DIR="/etc/bpfd"
    BPFD_RUNTIME_DIR="/run/bpfd"
    BPFD_RTDIR_FS="/run/bpfd/fs"
    BPFD_STATE_DIR="/var/lib/bpfd"

    # Remove directories
    if test -d "${BPFD_CONFIGURATION_DIR}"; then
        echo "Deleting \"bpfd\" specific directories"
        echo "  Deleting \"${BPFD_CONFIGURATION_DIR}\""
        rm -rf "${BPFD_CONFIGURATION_DIR}"
    fi
    if test -d "${BPFD_RUNTIME_DIR}"; then
        echo "  Deleting \"${BPFD_RUNTIME_DIR}\""
        umount "${BPFD_RTDIR_FS}"
        rm -rf "${BPFD_RUNTIME_DIR}"
    fi
    if test -d "${BPFD_STATE_DIR}"; then
        echo "  Deleting \"${BPFD_STATE_DIR}\""
        rm -rf "${BPFD_STATE_DIR}"
    fi
}

delete_user() {
    user_name=$1
    if [ -z "${user_name}" ]; then
        echo "User name required"
        exit 1
    fi
    user_group=$2
    if [ -z "${user_group}" ]; then
        user_group=${user_name}
    fi

    user_exists=false
    getent passwd ${user_name} &>/dev/null
    if [[ $? -eq 0 ]]; then
        echo "Deleting \"${user_name}:${user_group}\" user/group:"
        user_exists=true
    fi

    # Remove group from all users
    TMP_USER_LIST=($(cat /etc/group | grep ${user_group} | awk -F':' '{print $4}'))
    for USER in "${TMP_USER_LIST[@]}"
    do
        echo "  Removing ${user_group} from $USER"
        gpasswd -d "$USER" ${user_name}
    done

    # Delete User
    if [ "${user_exists}" == true ]; then
        echo "  Deleting user \"${user_name}\""
        userdel -r ${user_name}  &>/dev/null
    fi
}

user_cleanup() {
    delete_directories

    # TO BE REMOVED!
    # Left around to cleanup deprecated `bpfd` user and user group
    USER_BPFD="bpfd"
    USER_GROUP="bpfd"
    delete_user "${USER_BPFD}" "${USER_GROUP}"
    delete_bpfd_directories
}
