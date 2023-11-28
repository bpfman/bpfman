#!/bin/bash

copy_bin() {
    bin_name=$1
    if [ -z "${bin_name}" ]; then
        echo "Binary name required"
        exit 1
    fi
    release=$2
    if [ -z "${release}" ]; then
        release=false
    fi


    # To be backwards compatible, the logic is:
    # if "--release" was entered, only copy from the release directory,
    # if nothing was entered, try debug, if not there, try release.
    if [ "${release}" == false ] && test -f ${SRC_DEBUG_BIN_PATH}/${bin_name}; then
        src_path=${SRC_DEBUG_BIN_PATH}
    elif test -f ${SRC_RELEASE_BIN_PATH}/${bin_name}; then
        src_path=${SRC_RELEASE_BIN_PATH}
    else
        echo "  ERROR: Unable to find \"${bin_name}\" in \"${SRC_DEBUG_BIN_PATH}\" or \"${SRC_RELEASE_BIN_PATH}\""
        return
    fi

    echo "  Copying \"${src_path}/${bin_name}\" to \"${DST_BIN_PATH}\""
    cp "${src_path}/${bin_name}" "${DST_BIN_PATH}/${bin_name}"
}

del_bin() {
    bin_name=$1
    if [ -z "${bin_name}" ]; then
        echo "Binary name required"
        exit 1
    fi

    if test -f ${DST_BIN_PATH}/${bin_name}; then
        echo "  Removing \"${bin_name}\""
        rm -f "${DST_BIN_PATH}/${bin_name}"
    fi
}

copy_svc() {
    svc_name=$1
    if [ -z "${svc_name}" ]; then
        echo "service name required"
        exit 1
    fi

    echo "  Copying \"${svc_name}.service\""
    cp "${svc_name}.service" "${DST_SVC_PATH}/${svc_name}.service"
    systemctl daemon-reload
}

del_svc() {
    svc_name=$1
    if [ -z "${svc_name}" ]; then
        echo "service name required"
        exit 1
    fi

    systemctl status "${svc_name}" &>/dev/null
    if [ $? -eq 0 ]; then
        echo "  Stopping \"${svc_name}.service\""
        systemctl disable ${svc_name}.service
        systemctl stop ${svc_name}.service
    fi

    if test -f ${DST_SVC_PATH}/${svc_name}.service; then
        echo "  Removing \"${svc_name}.service\""
        rm -f "${DST_SVC_PATH}/${svc_name}.service"
    fi
}

install() {
    reinstall=$1
    if [ -z "${reinstall}" ]; then
        reinstall=false
    fi
    start_bpfman=$2
    if [ -z "${start_bpfman}" ]; then
        start_bpfman=false
    fi
    release=$3
    if [ -z "${release}" ]; then
        release=false
    fi

    echo "Copy binaries:"

    if [ "${reinstall}" == true ]; then
        systemctl status bpfman | grep "Active:" | grep running &>/dev/null
        if [ $? -eq 0 ]; then
            echo "  Stopping \"${BIN_BPFMAN}.service\""
            systemctl stop ${BIN_BPFMAN}.service
            start_bpfman=true
        fi
    fi

    copy_bin "${BIN_BPFMAN}" ${release}

    if [ "${reinstall}" == false ]; then
        echo "Copy service file:"
        copy_svc "${BIN_BPFMAN}"
        systemctl daemon-reload
    fi

    if [ "${start_bpfman}" == true ]; then
        echo "  Starting \"${BIN_BPFMAN}.service\""
        systemctl start ${BIN_BPFMAN}.service
    fi
}

uninstall() {
    echo "Remove service file:"
    del_svc "${BIN_BPFMAN}"

    echo "Remove binaries:"
    del_bin "${BIN_BPFMAN}"

    del_kubectl_plugin

    # TO BE REMOVED!
    # Left around to cleanup deprecated `bpfd` binary
    BIN_BPFD="bpfd"
    BIN_BPFCTL="bpfctl"
    del_svc "${BIN_BPFD}"
    del_bin "${BIN_BPFD}"
    del_bin "${BIN_BPFCTL}"
}

# TO BE REMOVED!
# Left around to cleanup deprecated kubectl plugins
del_kubectl_plugin() {
    if test -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs"; then
        echo "Remove kubectl plugins:"
        echo "  Deleting \"kubectl-bpfprogramconfigs\""
        rm -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprogramconfigs"
    fi

    if test -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms"; then
        echo "Remove kubectl plugins:"
        echo "  Deleting \"kubectl-bpfprograms\""
        rm -f "${DST_KUBECTL_PLUGIN_PATH}/kubectl-bpfprograms"
    fi
}
