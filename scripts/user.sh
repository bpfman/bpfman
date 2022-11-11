#!/bin/bash

create_user() {
    user_name=$1
    if [ -z "${user_name}" ]; then
        echo "User name required"
        exit 1
    fi
    user_group=$2
    if [ -z "${user_group}" ]; then
        user_group=${user_name}
    fi

    getent passwd $1 &>/dev/null
    if [[ $? -ne 0 ]]; then
        group_param=""
        if [ "${user_group}" != "${user_name}" ]; then
            group_param="-g ${user_group}"
        fi

        echo "  Creating user:group \"${user_name}:${user_group}\"  group_param=\"${group_param}\""
        useradd -r ${group_param} ${user_name}
    else
        echo "  User \"${user_name}\" already exists"
    fi
}

delete_user() {
    user_name=$1
    if [ -z "${user_name}" ]; then
        echo "User name required"
        exit 1
    fi

    BASE_PATH="/etc/${user_name}"

    # Remove directories
    echo "  Deleting \"${BASE_PATH}\""
    rm -rf "${BASE_PATH}"
    if [ "${user_name}" == "${USER_BPFD}" ]; then
        echo "  Deleting \"${user_name}\" specific directories"
        echo "  Deleting \"${RUNTIME_DIR}\""
        umount "${RTDIR_FS}"
        rm -rf "${RUNTIME_DIR}"
        echo "  Deleting \"${STATE_DIR}\""
        rm -rf "${STATE_DIR}"
    fi

    # Remove group from all users
    TMP_USER_LIST=($(cat /etc/group | grep ${user_name} | awk -F':' '{print $4}'))
    for USER in "${TMP_USER_LIST[@]}"
    do
        echo "  Removing ${user_name} from $USER"
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

user_init() {
    echo "Creating users/groups:"
    create_user "${USER_BPFD}" "${USER_GROUP}"
}

user_del() {
    echo "Deleting users/groups:"
    delete_user "${USER_BPFD}"

    # "bpfctl" no longer created. This can be removed later,
    # but left around to cleanup systems where bpfctl user was created.
    delete_user "bpfctl"
}
