#!/bin/bash

CALL_POPD=false
if [[ "$PWD" != */scripts ]]; then
    pushd scripts &>/dev/null
fi

# Source the functions in other files
. certificates.sh
. install.sh
. user.sh

BIN_BPFD="bpfd"
BIN_BPFCTL="bpfctl"
BIN_BPFD_CLIENT="bpfd-client"

# Well known directories
SRC_DEBUG_BIN_PATH="../target/debug"
SRC_RELEASE_BIN_PATH="../target/x86_64-unknown-linux-musl/release"
DST_BIN_PATH="/usr/sbin"
DST_SVC_PATH="/usr/lib/systemd/system"

# ConfigurationDirectory: /etc/bpfd/
CONFIGURATION_DIR="/etc/bpfd"
CFG_CA_CERT_DIR="/etc/bpfd/certs/ca"

# RuntimeDirectory: /run/bpfd/
RUNTIME_DIR="/run/bpfd"
RTDIR_FS="/run/bpfd/fs"

# StateDirectory: /var/lib/bpfd/
STATE_DIR="/var/lib/bpfd"


usage() {
    echo "USAGE:"
    echo "sudo ./scripts/setup.sh install [--release]"
    echo "    Prepare system for running \"bpfd\" as a systemd service. Performs the"
    echo "    following tasks:"
    echo "    * Copy \"bpfd\" and \"bpfctl\" binaries to \"/usr/sbin/.\"."
    echo "    * Copy \"bpfd.service\" to \"/usr/lib/systemd/system/\"."
    echo "    * Run \"systemctl start bpfd.service\" to start the sevice."
    echo "sudo ./scripts/setup.sh setup [--release]"
    echo "    Same as \"install\" above, but don't start the service."
    echo "sudo ./scripts/setup.sh reinstall [--release]"
    echo "    Only copy the \"bpfd\" and \"bpfctl\" binaries to \"/usr/sbin/.\""
    echo "    \"bpfd\" service will be restarted if it was running."
    echo "sudo ./scripts/setup.sh uninstall"
    echo "    Unwind all actions performed by \"setup.sh install\" including stopping"
    echo "    the \"bpfd\" service if it is running."
    echo "sudo ./scripts/setup.sh kubectl"
    echo "    Install kubectl plugins for \"bpfprogramconfigs\" and \"bpfprograms\"."
    echo "sudo ./scripts/setup.sh examples"
    echo "    Copy examples bytecode files to a bpfd owned directory (${RTDIR_EXAMPLES})."
    echo "    This assumes bytecode has already been built. \"setup.sh install\" does"
    echo "    this as well, so this is to overwrite after a rebuild."
}

if [ $USER != "root" ]; then
    echo "ERROR: \"root\" or \"sudo\" required."
    exit
fi

case "$1" in
    "install")
        reinstall=false
        start_bpfd=true
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfd} ${release}
        ;;
    "setup")
        reinstall=false
        start_bpfd=false
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfd} ${release}
        ;;
    "reinstall")
        reinstall=true
        # With reinstall true, start_bpfd will be set in function if bpfd is already running or not.
        start_bpfd=false
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfd} ${release}
        ;;
    "uninstall")
        uninstall
        user_cleanup
        ;;
    "certs")
        regen_cert=true
        cert_init ${regen_cert}
        ;;
    "help"|"--help"|"?")
        usage
        ;;
    *)
        echo "Unknown input: $1"
        echo
        usage
        ;;
esac

if [[ "$CALL_POPD" == true ]]; then
    popd &>/dev/null
fi
