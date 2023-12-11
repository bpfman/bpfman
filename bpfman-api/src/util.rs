// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

pub mod directories {
    // The following directories are used by bpfman. They should be created by bpfman service
    // via the bpfman.service settings. They will be manually created in the case where bpfman
    // is not being run as a service.
    //
    // ConfigurationDirectory: /etc/bpfman/
    pub const CFGDIR_MODE: u32 = 0o6750;
    pub const CFGDIR: &str = "/etc/bpfman";
    pub const CFGDIR_STATIC_PROGRAMS: &str = "/etc/bpfman/programs.d";
    pub const CFGPATH_BPFMAN_CONFIG: &str = "/etc/bpfman/bpfman.toml";
    pub const CFGPATH_CA_CERTS_PEM: &str = "/etc/bpfman/certs/ca/ca.pem";
    pub const CFGPATH_CA_CERTS_KEY: &str = "/etc/bpfman/certs/ca/ca.key";
    pub const CFGPATH_BPFMAN_CERTS_PEM: &str = "/etc/bpfman/certs/bpfman/bpfman.pem";
    pub const CFGPATH_BPFMAN_CERTS_KEY: &str = "/etc/bpfman/certs/bpfman/bpfman.key";
    pub const CFGPATH_BPFMAN_CLIENT_CERTS_PEM: &str =
        "/etc/bpfman/certs/bpfman-client/bpfman-client.pem";
    pub const CFGPATH_BPFMAN_CLIENT_CERTS_KEY: &str =
        "/etc/bpfman/certs/bpfman-client/bpfman-client.key";

    // RuntimeDirectory: /run/bpfman/
    pub const RTDIR_MODE: u32 = 0o6770;
    pub const RTDIR: &str = "/run/bpfman";
    pub const RTDIR_XDP_DISPATCHER: &str = "/run/bpfman/dispatchers/xdp";
    pub const RTDIR_TC_INGRESS_DISPATCHER: &str = "/run/bpfman/dispatchers/tc-ingress";
    pub const RTDIR_TC_EGRESS_DISPATCHER: &str = "/run/bpfman/dispatchers/tc-egress";
    pub const RTDIR_FS: &str = "/run/bpfman/fs";
    pub const RTDIR_FS_TC_INGRESS: &str = "/run/bpfman/fs/tc-ingress";
    pub const RTDIR_FS_TC_EGRESS: &str = "/run/bpfman/fs/tc-egress";
    pub const RTDIR_FS_XDP: &str = "/run/bpfman/fs/xdp";
    pub const RTDIR_FS_MAPS: &str = "/run/bpfman/fs/maps";
    pub const RTDIR_PROGRAMS: &str = "/run/bpfman/programs";
    pub const RTPATH_BPFMAN_SOCKET: &str = "/run/bpfman/bpfman.sock";
    // The CSI socket must be in it's own sub directory so we can easily create a dedicated
    // K8s volume mount for it.
    pub const RTDIR_BPFMAN_CSI: &str = "/run/bpfman/csi";
    pub const RTPATH_BPFMAN_CSI_SOCKET: &str = "/run/bpfman/csi/csi.sock";
    pub const RTDIR_BPFMAN_CSI_FS: &str = "/run/bpfman/csi/fs";

    // StateDirectory: /var/lib/bpfman/
    pub const STDIR_MODE: u32 = 0o6770;
    pub const STDIR: &str = "/var/lib/bpfman";
    pub const STDIR_DB: &str = "/var/lib/bpfman/db";
}
