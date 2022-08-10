#!/bin/bash

SRC_BIN_PATH="../target/debug"
DST_BIN_PATH="/usr/sbin"
DST_SVC_PATH="/usr/lib/systemd/system"

copy_bin() {
    bin_name=$1
    if [ -z "${bin_name}" ]; then
        echo "Binary name required"
        exit 1
    fi
    user_group=$2
    if [ -z "${user_group}" ]; then
        user_group=${bin_name}
    fi

    echo "  Copying \"${bin_name}\" and chown \"${bin_name}:${user_group}\""
    cp "${SRC_BIN_PATH}/${bin_name}" "${DST_BIN_PATH}/${bin_name}"
    chown ${bin_name}:${user_group} "${DST_BIN_PATH}/${bin_name}"
}

del_bin() {
    bin_name=$1
    if [ -z "${bin_name}" ]; then
        echo "Binary name required"
        exit 1
    fi
    echo "  Removing \"${bin_name}\""
    rm -f "${DST_BIN_PATH}/${bin_name}"
}

copy_svc() {
    svc_name=$1
    if [ -z "${svc_name}" ]; then
        echo "service name required"
        exit 1
    fi
    user_group=$2
    if [ -z "${user_group}" ]; then
        user_group=${svc_name}
    fi

    echo "  Copying \"${svc_name}.service\" and chown \"${svc_name}:${user_group}\""
    cp "${svc_name}.service" "${DST_SVC_PATH}/${svc_name}.service"
    chown ${svc_name}:${user_group} "${DST_SVC_PATH}/${svc_name}.service"
}

del_svc() {
    svc_name=$1
    if [ -z "${svc_name}" ]; then
        echo "service name required"
        exit 1
    fi

    echo "  Stopping \"${svc_name}.service\""
    systemctl stop ${svc_name}.service

    echo "  Removing \"${svc_name}.service\""
    rm -f "${DST_SVC_PATH}/${svc_name}.service"
}

install() {
    reinstall=$1
    if [ -z "${reinstall}" ]; then
        reinstall=false
    fi

    echo "Copy binaries:"

    START_BPFD=false
    if [ "${reinstall}" == true ]; then
        echo "  Stopping \"bpfd.service\""
        systemctl stop bpfd.service
        START_BPFD=true
    fi

    copy_bin "bpfd"
    copy_bin "bpfctl" "bpfd"

    if [ "${reinstall}" == false ]; then
        echo "Copy service file:"
        copy_svc "bpfd"
    else
        if [ "${START_BPFD}" == true ]; then
            echo "  Starting \"bpfd.service\""
            systemctl start bpfd.service
        fi
    fi
}

uninstall() {
    echo "Remove service file:"
    del_svc bpfd

    echo "Remove binaries:"
    del_bin bpfd
    del_bin bpfctl
}
