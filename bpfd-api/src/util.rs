// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

pub const USRGRP_BPFD: &str = "bpfd";

pub mod directories {
    // The following directories are used by bpfd. They should be created by bpfd service
    // via the bpfd.service settings. They will be manually created in the case where bpfd
    // is not being run as a service.
    //
    // ConfigurationDirectory: /etc/bpfd/
    pub const CFGDIR: &str = "/etc/bpfd";
    pub const CFGDIR_BPFD_CERTS: &str = "/etc/bpfd/certs/bpfd";
    pub const CFGDIR_BPFD_CLIENT_CERTS: &str = "/etc/bpfd/certs/bpfd-client";
    pub const CFGDIR_CA_CERTS: &str = "/etc/bpfd/certs/ca";
    pub const CFGDIR_STATIC_PROGRAMS: &str = "/etc/bpfd/programs.d";
    pub const CFGPATH_BPFD_CONFIG: &str = "/etc/bpfd/bpfd.toml";
    pub const CFGPATH_CA_CERTS_PEM: &str = "/etc/bpfd/certs/ca/ca.pem";
    pub const CFGPATH_CA_CERTS_KEY: &str = "/etc/bpfd/certs/ca/ca.key";
    pub const CFGPATH_BPFD_CERTS_PEM: &str = "/etc/bpfd/certs/bpfd/bpfd.pem";
    pub const CFGPATH_BPFD_CERTS_KEY: &str = "/etc/bpfd/certs/bpfd/bpfd.key";
    pub const CFGPATH_BPFD_CLIENT_CERTS_PEM: &str = "/etc/bpfd/certs/bpfd-client/bpfd-client.pem";
    pub const CFGPATH_BPFD_CLIENT_CERTS_KEY: &str = "/etc/bpfd/certs/bpfd-client/bpfd-client.key";
    // RuntimeDirectory: /run/bpfd/
    pub const RTDIR: &str = "/run/bpfd";
    pub const RTDIR_XDP_DISPATCHER: &str = "/run/bpfd/dispatchers/xdp";
    pub const RTDIR_TC_INGRESS_DISPATCHER: &str = "/run/bpfd/dispatchers/tc-ingress";
    pub const RTDIR_TC_EGRESS_DISPATCHER: &str = "/run/bpfd/dispatchers/tc-egress";
    pub const RTDIR_FS: &str = "/run/bpfd/fs";
    pub const RTDIR_FS_TC_INGRESS: &str = "/run/bpfd/fs/tc-ingress";
    pub const RTDIR_FS_TC_EGRESS: &str = "/run/bpfd/fs/tc-egress";
    pub const RTDIR_FS_XDP: &str = "/run/bpfd/fs/xdp";
    pub const RTDIR_FS_MAPS: &str = "/run/bpfd/fs/maps";
    pub const RTDIR_PROGRAMS: &str = "/run/bpfd/programs";
    // StateDirectory: /var/lib/bpfd/
    pub const STDIR: &str = "/var/lib/bpfd";
    pub const STDIR_SOCKET: &str = "/var/lib/bpfd/sock";
    pub const STPATH_BPFD_SOCKET: &str = "/var/lib/bpfd/sock/bpfd.sock";
    pub const BYTECODE_IMAGE_CONTENT_STORE: &str = "/var/lib/bpfd/io.bpfd.image.content";
}
