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

create_user_dirs() {
    user_name=$1
    if [ -z "${user_name}" ]; then
        echo "User name required"
        exit 1
    fi
    user_group=$2
    if [ -z "${user_group}" ]; then
        user_group=${user_name}
    fi

    BASE_PATH="/etc/${user_name}"

    echo "  Ensure \"${BASE_PATH}\" exists"
    mkdir -p "${BASE_PATH}"

    # Set the owner if the user has been created.
    getent passwd $1 &>/dev/null
    if [[ $? -eq 0 ]]; then
        echo "  chown and chmod of \"${BASE_PATH}\""
        chown -R ${user_name}:${user_group} "${BASE_PATH}"
        # Set the setuid and setgid bits so that files under directory
        # will inherit user group.
        chmod 6755 "${BASE_PATH}"
    fi

    if [ ! -f "${BASE_PATH}/${user_name}.toml" ]; then
        echo "  Copying \"${user_name}.toml\""
        cp "${user_name}.toml" "${BASE_PATH}/."
    fi

    if [ "${user_name}" == "bpfd" ]; then
        echo "  Creating \"${user_name}\" specific directories"
        mkdir -p "${BASE_PATH}/bytecode/"
        mkdir -p "${BASE_PATH}/programs.d/"
        mkdir -p "${BASE_PATH}/sock/"
        # Set the setuid and setgid bits (6000) so that files under sock directory
        # will inherit user group. Also set the sock directory so any of the "bpfd"
        # group can read and write to a sock (0670).
        chmod -R 6775 "${BASE_PATH}/sock/"
        setfacl -d -m u:${user_name}:rwx,g:${user_group}:rwx,o::- "${BASE_PATH}/sock/"
    fi
}

user_init() {
    echo "Creating users/groups:"
    create_user "bpfd"
    create_user_dirs "bpfd"
    create_user "bpfctl" "bpfd"
    create_user_dirs "bpfctl" "bpfd"
}

user_del() {
    echo "Deleting users/groups:"
    delete_user "bpfctl"
    delete_user "bpfd"

    echo "  Deleting legacy files"
    rm -f /etc/bpfd.toml
}

user_dir() {
    echo "Creating users directories:"
    create_user_dirs "bpfd"
    create_user_dirs "bpfctl" "bpfd"
}
