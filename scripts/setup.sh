#!/bin/bash

CALL_POPD=false
if [[ "$PWD" != */scripts ]]; then
    pushd scripts &>/dev/null || exit
fi

# Source the functions in other files
. certificates.sh
. install.sh
. user.sh

BIN_BPFMAN="bpfman"
BIN_BPFMAN_RPC="bpfman-rpc"
BIN_BPFMAN_CLIENT="bpfman-client"

# Well known directories
SRC_DEBUG_BIN_PATH="../target/debug"
SRC_RELEASE_BIN_PATH="../target/x86_64-unknown-linux-musl/release"
DST_BIN_PATH="/usr/sbin"
DST_SVC_PATH="/usr/lib/systemd/system"
SVC_BPFMAN_SOCK="${BIN_BPFMAN}.socket"
SVC_BPFMAN_SVC="${BIN_BPFMAN}.service"

SRC_CLI_TAB_COMPLETE_PATH="../.output/completions"
DST_CLI_TAB_COMPLETE_PATH="/usr/share/bash-completion/completions"

SRC_MANPAGE_PATH="../.output/manpage"
DST_MANPAGE_PATH="/usr/local/share/man/man1"

# ConfigurationDirectory: /etc/bpfman/
CONFIGURATION_DIR="/etc/bpfman"
CFG_CA_CERT_DIR="/etc/bpfman/certs/ca"

# RuntimeDirectory: /run/bpfman/
RUNTIME_DIR="/run/bpfman"
RTDIR_FS="/run/bpfman/fs"
RUNTIME_SOCKET_DIR="/run/bpfman-sock"

# StateDirectory: /var/lib/bpfman/
STATE_DIR="/var/lib/bpfman"


usage() {
    echo "USAGE:"
    echo "sudo ./scripts/setup.sh install [--release]"
    echo "    Prepare system for running \"bpfman\" as a systemd service. Performs the"
    echo "    following tasks:"
    echo "    * Copy \"bpfman\" binaries to \"/usr/sbin/.\"."
    echo "    * Copy \"bpfman\" CLI TAB completeion files to"
    echo "       \"/usr/share/bash-completion/completions/.\", if they have been generated."
    echo "    * Copy \"bpfman\" manpages to \"/usr/local/share/man/man1/.\", if they have"
    echo "      been generated."
    echo "    * Copy \"bpfman.service\" to \"/usr/lib/systemd/system/\"."
    echo "    * Run \"systemctl start bpfman.socket\" to start the sevice."
    echo "sudo ./scripts/setup.sh setup [--release]"
    echo "    Same as \"install\" above, but don't start the service."
    echo "sudo ./scripts/setup.sh reinstall [--release]"
    echo "    Only copy the \"bpfman\" binaries to \"/usr/sbin/.\""
    echo "    \"bpfman\" service will be restarted if it was running."
    echo "sudo ./scripts/setup.sh uninstall"
    echo "    Unwind all actions performed by \"setup.sh install\" including stopping"
    echo "    the \"bpfman\" service if it is running."
    echo "sudo ./scripts/setup.sh kubectl"
    echo "    Install kubectl plugins for \"bpfprogramconfigs\" and \"bpfprograms\"."
    echo "sudo ./scripts/setup.sh examples"
    echo "    Copy examples bytecode files to a bpfman owned directory (${RTDIR_EXAMPLES})."
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
        start_bpfman=true
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfman} ${release}
        ;;
    "setup")
        reinstall=false
        start_bpfman=false
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfman} ${release}
        ;;
    "reinstall")
        reinstall=true
        # With reinstall true, start_bpfman will be set in function if bpfman is already running or not.
        start_bpfman=false
        release=false
        if [ "$2" == "--release" ] || [ "$2" == "release" ] ; then
            release=true
        fi
        install ${reinstall} ${start_bpfman} ${release}
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
    popd &>/dev/null || exit
fi
