#!/bin/bash

copy_bin() {
    bin_name=$1
    if [ -z "${bin_name}" ]; then
        echo "Binary name required"
        exit 1
    fi
    user_name=$2
    if [ -z "${user_name}" ]; then
        user_name=${bin_name}
    fi
    user_group=$3
    if [ -z "${user_group}" ]; then
        user_group=${bin_name}
    fi

    echo "  Copying \"${bin_name}\" and chown \"${user_name}:${user_group}\""
    cp "${SRC_BIN_PATH}/${bin_name}" "${DST_BIN_PATH}/${bin_name}"
    chown ${user_name}:${user_group} "${DST_BIN_PATH}/${bin_name}"
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
    user_name=$2
    if [ -z "${user_name}" ]; then
        user_name=${svc_name}
    fi
    user_group=$3
    if [ -z "${user_group}" ]; then
        user_group=${svc_name}
    fi

    echo "  Copying \"${svc_name}.service\" and chown \"${user_name}:${user_group}\""
    cp "${svc_name}.service" "${DST_SVC_PATH}/${svc_name}.service"
    chown ${user_name}:${user_group} "${DST_SVC_PATH}/${svc_name}.service"
    systemctl daemon-reload
}

del_svc() {
    svc_name=$1
    if [ -z "${svc_name}" ]; then
        echo "service name required"
        exit 1
    fi

    echo "  Stopping \"${svc_name}.service\""
    systemctl disable ${svc_name}.service
    systemctl stop ${svc_name}.service

    echo "  Removing \"${svc_name}.service\""
    rm -f "${DST_SVC_PATH}/${svc_name}.service"
}

copy_examples_bytecode() {
    example_name=$1
    if [ -z "${example_name}" ]; then
        echo "Example name required"
        exit 1
    fi
    bytecode_name=$2
    if [ -z "${bytecode_name}" ]; then
        echo "Binary name required"
        exit 1
    fi
    user_name=$3
    if [ -z "${user_name}" ]; then
        user_name=${bin_name}
    fi
    user_group=$4
    if [ -z "${user_group}" ]; then
        user_group=${bin_name}
    fi

    echo "  Copying \"${example_name}/${bytecode_name}\" and chown \"${user_name}:${user_group}\""
    mkdir -p "${RTDIR_EXAMPLES}/${example_name}"
    cp "${SRC_EXAMPLE_PATH}/${example_name}/${bytecode_name}" "${RTDIR_EXAMPLES}/${example_name}/${bytecode_name}"
    chown -R ${user_name}:${user_group} "${RTDIR_EXAMPLES}"
}

copy_examples() {
    copy_examples_bytecode "${SRC_EXAMPLE_TC}" "${SRC_EXAMPLE_BYTECODE}" "${USER_BPFD}" "${USER_GROUP}"
    copy_examples_bytecode "${SRC_EXAMPLE_TRACEPOINT}" "${SRC_EXAMPLE_BYTECODE}" "${USER_BPFD}" "${USER_GROUP}"
    copy_examples_bytecode "${SRC_EXAMPLE_XDP}" "${SRC_EXAMPLE_BYTECODE}" "${USER_BPFD}" "${USER_GROUP}"
}

install() {
    reinstall=$1
    if [ -z "${reinstall}" ]; then
        reinstall=false
    fi

    echo "Copy binaries:"

    START_BPFD=false
    if [ "${reinstall}" == true ]; then
        echo "  Stopping \"${BIN_BPFD}.service\""
        systemctl stop ${BIN_BPFD}.service
        START_BPFD=true
    fi

    copy_bin "${BIN_BPFD}" "${USER_BPFD}" "${USER_GROUP}"
    copy_bin "${BIN_BPFCTL}" "${USER_BPFD}" "${USER_GROUP}"

    if [ "${reinstall}" == false ]; then
        echo "Copy service file:"
        copy_svc "${BIN_BPFD}" "${USER_BPFD}" "${USER_GROUP}"
        systemctl daemon-reload
        echo "  Starting \"${BIN_BPFD}.service\""
        systemctl start ${BIN_BPFD}.service
    else
        if [ "${START_BPFD}" == true ]; then
            echo "  Starting \"${BIN_BPFD}.service\""
            systemctl start ${BIN_BPFD}.service
        fi
    fi

    echo "Copy examples bytecode:"
    copy_examples
}

uninstall() {
    echo "Remove service file:"
    # This can be removed at a future date. bpfctl service no longer started,
    # But left here to cleanup on systems where is has been deployed.
    del_svc "${BIN_BPFCTL}"
    del_svc "${BIN_BPFD}"

    echo "Remove binaries:"
    del_bin "${BIN_BPFCTL}"
    del_bin "${BIN_BPFD}"
}

copy_kubectl_plugin() {
    echo "Installing kubectl plugins:"

    echo "  Copying \"kubectl-bpfprogramconfigs\""
    cp "${SRC_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs" "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs"
    chown root:root "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs"

    echo "  Copying \"kubectl-bpfprograms\""
    cp "${SRC_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms" "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms"
    chown root:root "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms"
}

del_kubectl_plugin() {
    echo "Remove kubectl plugins:"

    echo "  Deleting \"kubectl-bpfprogramconfigs\""
    rm -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs"

    echo "  Deleting \"kubectl-bpfprograms\""
    rm -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms"
}
