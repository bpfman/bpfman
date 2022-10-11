// SPDX-License-Identifier: (MIT OR Apache-2.0)
// Copyright Authors of bpfd

#[rustfmt::skip]
pub mod directories {
    // The following directories are used by bpfd. They should be created by bpfd service
    // via the bpfd.service settings. They will be manually created in the case where bpfd
    // is not being run as a service.
    //
    // ConfigurationDirectory: /etc/bpfd/
    pub const CFGDIR:                 &str = "/etc/bpfd";
    pub const CFGDIR_BPFD_CERTS:      &str = "/etc/bpfd/certs/bpfd";
    pub const CFGDIR_CA_CERTS:        &str = "/etc/bpfd/certs/ca";
    pub const CFGDIR_STATIC_PROGRAMS: &str = "/etc/bpfd/programs.d";
    pub const CFGPATH_BPFD_CONFIG:    &str = "/etc/bpfd/bpfd.toml";
    pub const CFGPATH_CA_CERTS_PEM:   &str = "/etc/bpfd/certs/ca/ca.pem";
    pub const CFGPATH_CA_CERTS_KEY:   &str = "/etc/bpfd/certs/ca/ca.key";
    pub const CFGPATH_BPFD_CERTS_PEM: &str = "/etc/bpfd/certs/bpfd/bpfd.pem";
    pub const CFGPATH_BPFD_CERTS_KEY: &str = "/etc/bpfd/certs/bpfd/bpfd.key";
    //
    // RuntimeDirectory: /run/bpfd/
    pub const RTDIR:            &str = "/run/bpfd";
    pub const RTDIR_BYTECODE:   &str = "/run/bpfd/bytecode";
    pub const RTDIR_DISPATCHER: &str = "/run/bpfd/dispatchers";
    pub const RTDIR_FS:         &str = "/run/bpfd/fs";
    pub const RTDIR_FS_MAPS:    &str = "/run/bpfd/fs/maps";
    pub const RTDIR_PROGRAMS:   &str = "/run/bpfd/programs";
    //
    // CacheDirectory: /var/cache/
    //
    // StateDirectory: /var/lib/bpfd/
    pub const STDIR:        &str = "/var/lib/bpfd";
    pub const STDIR_SOCKET: &str = "/var/lib/bpfd/sock";
    //
    // LogsDirectory:  /var/log/

    // The following directories are used by bpfctl. They should be created by bpfctl service
    // via the bpfctl.service settings. They will be manually created in the case where bpfctl
    // is not run as a service.
    //
    // ConfigurationDirectory: /etc/bpfctl/
    pub const CFGDIR_BPFCTL:            &str = "/etc/bpfctl";
    pub const CFGDIR_BPFCTL_CERTS:      &str = "/etc/bpfctl/certs/bpfctl";
    pub const CFGPATH_BPFCTL_CERTS_PEM: &str = "/etc/bpfctl/certs/bpfctl/bpfctl.pem";
    pub const CFGPATH_BPFCTL_CERTS_KEY: &str = "/etc/bpfctl/certs/bpfctl/bpfctl.key";
    pub const CFGPATH_BPFCTL_CONFIG:    &str = "/etc/bpfctl/bpfctl.toml";
}
