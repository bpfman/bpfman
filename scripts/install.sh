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

    echo "  Copying \"${src_path}/${bin_name}\" to \"${DST_BIN_PATH}/.\""
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
    svc_file=$1
    if [ -z "${svc_file}" ]; then
        echo "service file required"
        exit 1
    fi

    echo "  Copying \"${svc_file}\" to \"${DST_SVC_PATH}/.\""
    cp "${svc_file}" "${DST_SVC_PATH}/${svc_file}"
    systemctl daemon-reload
}

del_svc() {
    svc_file=$1
    if [ -z "${svc_file}" ]; then
        echo "service file required"
        exit 1
    fi

    systemctl status "${svc_file}" &>/dev/null
    if [ $? -eq 0 ]; then
        echo "  Stopping \"${svc_file}\""
        systemctl disable ${svc_file}
        systemctl stop ${svc_file}
    fi

    if test -f ${DST_SVC_PATH}/${svc_file}; then
        echo "  Removing \"${svc_file}\""
        rm -f "${DST_SVC_PATH}/${svc_file}"
    fi
}

copy_cli_tab_completion() {
    if [ -d ${SRC_CLI_TAB_COMPLETE_PATH} ] && [ "$(ls -A ${SRC_CLI_TAB_COMPLETE_PATH})" ]; then
    #if [ -d ${SRC_CLI_TAB_COMPLETE_PATH} ] && [ "$(find ${SRC_CLI_TAB_COMPLETE_PATH} -mindepth 1 -maxdepth 1)" ]; then
        case $SHELL in
        "/bin/bash")
            echo "  Copying \"${SRC_CLI_TAB_COMPLETE_PATH}/bpfman.bash\" to \"${DST_CLI_TAB_COMPLETE_PATH}/.\""
            cp ${SRC_CLI_TAB_COMPLETE_PATH}/bpfman.bash ${DST_CLI_TAB_COMPLETE_PATH}/.
            ;;

        *)
            echo "Currently only bash is supported by this script. For other shells, manually install."
            ;;
        esac


    else
        echo "  CLI TAB Completion files not generated yet. Use \"cargo xtask build-completion\" to generate."
    fi
}

del_cli_tab_completion() {
    if [ -d ${DST_CLI_TAB_COMPLETE_PATH} ] && [ -f ${DST_CLI_TAB_COMPLETE_PATH}/bpfman.bash ]; then
        echo "  Removing CLI TAB Completion files from \"${DST_CLI_TAB_COMPLETE_PATH}/bpfman.bash\""
        rm ${DST_CLI_TAB_COMPLETE_PATH}/bpfman.bash &>/dev/null
    fi
}

copy_manpages() {
    if [ -d ${SRC_MANPAGE_PATH} ] && [ "$(ls -A ${SRC_MANPAGE_PATH})" ]; then
    #if [ -d ${SRC_MANPAGE_PATH} ] && [ -z "$(find ${SRC_MANPAGE_PATH} -mindepth 1 -maxdepth 1)" ]; then
        echo "  Copying \"${SRC_MANPAGE_PATH}/*\" to \"${DST_MANPAGE_PATH}/.\""
        rm ${DST_MANPAGE_PATH}/bpfman*.1  &>/dev/null
        cp ${SRC_MANPAGE_PATH}/bpfman*.1 ${DST_MANPAGE_PATH}/.
    else
        echo "  CLI Manpage files not generated yet. Use \"cargo xtask build-man-page\" to generate."
    fi
}

del_manpages() {
    if [ -d ${DST_MANPAGE_PATH} ] && [ -f ${DST_MANPAGE_PATH}/bpfman.1 ]; then
        echo "  Removing Manpage files from \"${DST_MANPAGE_PATH}\""
        rm ${DST_MANPAGE_PATH}/bpfman*.1 &>/dev/null
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

    echo "Copy CLI TAB Completion files:"
    copy_cli_tab_completion

    echo "Copy Manpage files:"
    copy_manpages

    echo "Copy binaries:"

    if [ "${reinstall}" == true ]; then
        systemctl status bpfman | grep "Active:" | grep running &>/dev/null
        if [ $? -eq 0 ]; then
            echo "  Stopping \"${SVC_BPFMAN_SVC}\""
            systemctl stop ${SVC_BPFMAN_SVC}
            start_bpfman=true
        fi
    fi

    copy_bin "${BIN_BPFMAN}" ${release}
    copy_bin "${BIN_BPFMAN_RPC}" ${release}
    copy_bin "${BIN_BPFMAN_NS}" ${release}
    copy_bin "${BIN_BPF_LOG_EXPORTER}" ${release}
    copy_bin "${BIN_BPF_METRICS_EXPORTER}" ${release}

    if [ "${reinstall}" == false ]; then
        echo "Copy service files:"
        copy_svc "${SVC_BPFMAN_SOCK}"
        copy_svc "${SVC_BPFMAN_SVC}"
    fi

    if [ "${start_bpfman}" == true ]; then
        echo "  Starting \"${SVC_BPFMAN_SOCK}\""
        systemctl enable --now ${SVC_BPFMAN_SOCK}
    fi
}

uninstall() {
    echo "Remove CLI TAB Completion files:"
    del_cli_tab_completion

    echo "Remove Manpage files:"
    del_manpages

    echo "Remove service files:"
    del_svc "${SVC_BPFMAN_SOCK}"
    del_svc "${SVC_BPFMAN_SVC}"

    echo "Remove binaries:"
    del_bin "${BIN_BPF_METRICS_EXPORTER}"
    del_bin "${BIN_BPF_LOG_EXPORTER}"
    del_bin "${BIN_BPFMAN}"
    del_bin "${BIN_BPFMAN_RPC}"
    del_bin "${BIN_BPFMAN_NS}"

    del_kubectl_plugin

    # TO BE REMOVED!
    # Left around to cleanup deprecated `bpfd` binary
    SVC_BPFD_SVC="bpfd.service"
    BIN_BPFD="bpfd"
    BIN_BPFCTL="bpfctl"
    del_svc "${SVC_BPFD_SVC}"
    del_bin "${BIN_BPFD}"
    del_bin "${BIN_BPFCTL}"

    # Delete clsact and ingress qdiscs, in the default namespace and all network
    # namespaces.  This has the side-effect of removing any tc_dispatchers (or
    # other programs) that happen to be attached to them.
    echo "Delete qdiscs:"
    delete_all_bpfman_qdiscs
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

delete_bpfman_qdiscs() {
    local iface
    for iface in $(ifconfig | cut -d ' ' -f1 | tr ':' '\n' | awk NF); do
        # delete the clsact qdisc if it exists
        sudo tc qdisc del dev "$iface" clsact 2>/dev/null
        # delete the ingress qdisc if it exists
        sudo tc qdisc del dev "$iface" ingress 2>/dev/null
    done
}

delete_all_bpfman_qdiscs() {
    # Delete qdiscs in the default namespace
    delete_bpfman_qdiscs

    # Loop through all network namespaces and delete qdiscs
    for netns in $(ip netns list | cut -d ' ' -f1); do
        echo "  Processing namespace: $netns"
        ip netns exec "$netns" bash -c "$(declare -f delete_bpfman_qdiscs); delete_bpfman_qdiscs"
    done
}
