#!/bin/bash

delete_directories() {
    # Remove directories
    echo "Deleting \"bpfman\" specific directories"
    echo "  Deleting \"${CONFIGURATION_DIR}\""
    rm -rf "${CONFIGURATION_DIR}"
    echo "  Deleting \"${RUNTIME_DIR}\""
    umount "${RTDIR_FS}"
    rm -rf "${RUNTIME_DIR}"
    echo "  Deleting \"${STATE_DIR}\""
    rm -rf "${STATE_DIR}"
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

    echo "Deleting \"${user_name}:${user_group}\" user/group:"

    # Remove group from all users
    TMP_USER_LIST=($(cat /etc/group | grep ${user_group} | awk -F':' '{print $4}'))
    for USER in "${TMP_USER_LIST[@]}"
    do
        echo "  Removing ${user_group} from $USER"
        gpasswd -d "$USER" ${user_name}
    done

    # Delete User
    getent passwd ${user_name} &>/dev/null
    if [[ $? -eq 0 ]]; then
        echo "  Deleting user \"${user_name}\""
        userdel -r ${user_name}  &>/dev/null
    else
        echo "  User \"${user_name}\" does not exist"
    fi
}

user_cleanup() {
    delete_directories

    # TO BE REMOVED!
    # Left around to cleanup deprecated `bpfman` user and user group
    USER_BPFMAN="bpfman"
    USER_GROUP="bpfman"
    delete_user "${USER_BPFMAN}" "${USER_GROUP}"
}
