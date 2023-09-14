/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetPluginInfoRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetPluginInfoResponse {
    /// The name MUST follow domain name notation format
    /// (<https://tools.ietf.org/html/rfc1035#section-2.3.1>). It SHOULD
    /// include the plugin's host company name and the plugin name,
    /// to minimize the possibility of collisions. It MUST be 63
    /// characters or less, beginning and ending with an alphanumeric
    /// character (\[a-z0-9A-Z\]) with dashes (-), dots (.), and
    /// alphanumerics between. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub name: ::prost::alloc::string::String,
    /// This field is REQUIRED. Value of this field is opaque to the CO.
    #[prost(string, tag = "2")]
    pub vendor_version: ::prost::alloc::string::String,
    /// This field is OPTIONAL. Values are opaque to the CO.
    #[prost(map = "string, string", tag = "3")]
    pub manifest: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetPluginCapabilitiesRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetPluginCapabilitiesResponse {
    /// All the capabilities that the controller service supports. This
    /// field is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub capabilities: ::prost::alloc::vec::Vec<PluginCapability>,
}
/// Specifies a capability of the plugin.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct PluginCapability {
    #[prost(oneof = "plugin_capability::Type", tags = "1, 2")]
    pub r#type: ::core::option::Option<plugin_capability::Type>,
}
/// Nested message and enum types in `PluginCapability`.
pub mod plugin_capability {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Service {
        #[prost(enumeration = "service::Type", tag = "1")]
        pub r#type: i32,
    }
    /// Nested message and enum types in `Service`.
    pub mod service {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Type {
            Unknown = 0,
            /// CONTROLLER_SERVICE indicates that the Plugin provides RPCs for
            /// the ControllerService. Plugins SHOULD provide this capability.
            /// In rare cases certain plugins MAY wish to omit the
            /// ControllerService entirely from their implementation, but such
            /// SHOULD NOT be the common case.
            /// The presence of this capability determines whether the CO will
            /// attempt to invoke the REQUIRED ControllerService RPCs, as well
            /// as specific RPCs as indicated by ControllerGetCapabilities.
            ControllerService = 1,
            /// VOLUME_ACCESSIBILITY_CONSTRAINTS indicates that the volumes for
            /// this plugin MAY NOT be equally accessible by all nodes in the
            /// cluster. The CO MUST use the topology information returned by
            /// CreateVolumeRequest along with the topology information
            /// returned by NodeGetInfo to ensure that a given volume is
            /// accessible from a given node when scheduling workloads.
            VolumeAccessibilityConstraints = 2,
            /// GROUP_CONTROLLER_SERVICE indicates that the Plugin provides
            /// RPCs for operating on groups of volumes. Plugins MAY provide
            /// this capability.
            /// The presence of this capability determines whether the CO will
            /// attempt to invoke the REQUIRED GroupController service RPCs, as
            /// well as specific RPCs as indicated by
            /// GroupControllerGetCapabilities.
            GroupControllerService = 3,
        }
        impl Type {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Type::Unknown => "UNKNOWN",
                    Type::ControllerService => "CONTROLLER_SERVICE",
                    Type::VolumeAccessibilityConstraints => {
                        "VOLUME_ACCESSIBILITY_CONSTRAINTS"
                    }
                    Type::GroupControllerService => "GROUP_CONTROLLER_SERVICE",
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "CONTROLLER_SERVICE" => Some(Self::ControllerService),
                    "VOLUME_ACCESSIBILITY_CONSTRAINTS" => {
                        Some(Self::VolumeAccessibilityConstraints)
                    }
                    "GROUP_CONTROLLER_SERVICE" => Some(Self::GroupControllerService),
                    _ => None,
                }
            }
        }
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct VolumeExpansion {
        #[prost(enumeration = "volume_expansion::Type", tag = "1")]
        pub r#type: i32,
    }
    /// Nested message and enum types in `VolumeExpansion`.
    pub mod volume_expansion {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Type {
            Unknown = 0,
            /// ONLINE indicates that volumes may be expanded when published to
            /// a node. When a Plugin implements this capability it MUST
            /// implement either the EXPAND_VOLUME controller capability or the
            /// EXPAND_VOLUME node capability or both. When a plugin supports
            /// ONLINE volume expansion and also has the EXPAND_VOLUME
            /// controller capability then the plugin MUST support expansion of
            /// volumes currently published and available on a node. When a
            /// plugin supports ONLINE volume expansion and also has the
            /// EXPAND_VOLUME node capability then the plugin MAY support
            /// expansion of node-published volume via NodeExpandVolume.
            ///
            /// Example 1: Given a shared filesystem volume (e.g. GlusterFs),
            ///    the Plugin may set the ONLINE volume expansion capability and
            ///    implement ControllerExpandVolume but not NodeExpandVolume.
            ///
            /// Example 2: Given a block storage volume type (e.g. EBS), the
            ///    Plugin may set the ONLINE volume expansion capability and
            ///    implement both ControllerExpandVolume and NodeExpandVolume.
            ///
            /// Example 3: Given a Plugin that supports volume expansion only
            ///    upon a node, the Plugin may set the ONLINE volume
            ///    expansion capability and implement NodeExpandVolume but not
            ///    ControllerExpandVolume.
            Online = 1,
            /// OFFLINE indicates that volumes currently published and
            /// available on a node SHALL NOT be expanded via
            /// ControllerExpandVolume. When a plugin supports OFFLINE volume
            /// expansion it MUST implement either the EXPAND_VOLUME controller
            /// capability or both the EXPAND_VOLUME controller capability and
            /// the EXPAND_VOLUME node capability.
            ///
            /// Example 1: Given a block storage volume type (e.g. Azure Disk)
            ///    that does not support expansion of "node-attached" (i.e.
            ///    controller-published) volumes, the Plugin may indicate
            ///    OFFLINE volume expansion support and implement both
            ///    ControllerExpandVolume and NodeExpandVolume.
            Offline = 2,
        }
        impl Type {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Type::Unknown => "UNKNOWN",
                    Type::Online => "ONLINE",
                    Type::Offline => "OFFLINE",
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "ONLINE" => Some(Self::Online),
                    "OFFLINE" => Some(Self::Offline),
                    _ => None,
                }
            }
        }
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Type {
        /// Service that the plugin supports.
        #[prost(message, tag = "1")]
        Service(Service),
        #[prost(message, tag = "2")]
        VolumeExpansion(VolumeExpansion),
    }
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ProbeRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ProbeResponse {
    /// Readiness allows a plugin to report its initialization status back
    /// to the CO. Initialization for some plugins MAY be time consuming
    /// and it is important for a CO to distinguish between the following
    /// cases:
    ///
    /// 1) The plugin is in an unhealthy state and MAY need restarting. In
    ///     this case a gRPC error code SHALL be returned.
    /// 2) The plugin is still initializing, but is otherwise perfectly
    ///     healthy. In this case a successful response SHALL be returned
    ///     with a readiness value of `false`. Calls to the plugin's
    ///     Controller and/or Node services MAY fail due to an incomplete
    ///     initialization state.
    /// 3) The plugin has finished initializing and is ready to service
    ///     calls to its Controller and/or Node services. A successful
    ///     response is returned with a readiness value of `true`.
    ///
    /// This field is OPTIONAL. If not present, the caller SHALL assume
    /// that the plugin is in a ready state and is accepting calls to its
    /// Controller and/or Node services (according to the plugin's reported
    /// capabilities).
    #[prost(message, optional, tag = "1")]
    pub ready: ::core::option::Option<bool>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateVolumeRequest {
    /// The suggested name for the storage space. This field is REQUIRED.
    /// It serves two purposes:
    /// 1) Idempotency - This name is generated by the CO to achieve
    ///     idempotency.  The Plugin SHOULD ensure that multiple
    ///     `CreateVolume` calls for the same name do not result in more
    ///     than one piece of storage provisioned corresponding to that
    ///     name. If a Plugin is unable to enforce idempotency, the CO's
    ///     error recovery logic could result in multiple (unused) volumes
    ///     being provisioned.
    ///     In the case of error, the CO MUST handle the gRPC error codes
    ///     per the recovery behavior defined in the "CreateVolume Errors"
    ///     section below.
    ///     The CO is responsible for cleaning up volumes it provisioned
    ///     that it no longer needs. If the CO is uncertain whether a volume
    ///     was provisioned or not when a `CreateVolume` call fails, the CO
    ///     MAY call `CreateVolume` again, with the same name, to ensure the
    ///     volume exists and to retrieve the volume's `volume_id` (unless
    ///     otherwise prohibited by "CreateVolume Errors").
    /// 2) Suggested name - Some storage systems allow callers to specify
    ///     an identifier by which to refer to the newly provisioned
    ///     storage. If a storage system supports this, it can optionally
    ///     use this name as the identifier for the new volume.
    /// Any Unicode string that conforms to the length limit is allowed
    /// except those containing the following banned characters:
    /// U+0000-U+0008, U+000B, U+000C, U+000E-U+001F, U+007F-U+009F.
    /// (These are control characters other than commonly used whitespace.)
    #[prost(string, tag = "1")]
    pub name: ::prost::alloc::string::String,
    /// This field is OPTIONAL. This allows the CO to specify the capacity
    /// requirement of the volume to be provisioned. If not specified, the
    /// Plugin MAY choose an implementation-defined capacity range. If
    /// specified it MUST always be honored, even when creating volumes
    /// from a source; which MAY force some backends to internally extend
    /// the volume after creating it.
    #[prost(message, optional, tag = "2")]
    pub capacity_range: ::core::option::Option<CapacityRange>,
    /// The capabilities that the provisioned volume MUST have. SP MUST
    /// provision a volume that will satisfy ALL of the capabilities
    /// specified in this list. Otherwise SP MUST return the appropriate
    /// gRPC error code.
    /// The Plugin MUST assume that the CO MAY use the provisioned volume
    /// with ANY of the capabilities specified in this list.
    /// For example, a CO MAY specify two volume capabilities: one with
    /// access mode SINGLE_NODE_WRITER and another with access mode
    /// MULTI_NODE_READER_ONLY. In this case, the SP MUST verify that the
    /// provisioned volume can be used in either mode.
    /// This also enables the CO to do early validation: If ANY of the
    /// specified volume capabilities are not supported by the SP, the call
    /// MUST return the appropriate gRPC error code.
    /// This field is REQUIRED.
    #[prost(message, repeated, tag = "3")]
    pub volume_capabilities: ::prost::alloc::vec::Vec<VolumeCapability>,
    /// Plugin specific creation-time parameters passed in as opaque
    /// key-value pairs. This field is OPTIONAL. The Plugin is responsible
    /// for parsing and validating these parameters. COs will treat
    /// these as opaque.
    #[prost(map = "string, string", tag = "4")]
    pub parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Secrets required by plugin to complete volume creation request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "5")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// If specified, the new volume will be pre-populated with data from
    /// this source. This field is OPTIONAL.
    #[prost(message, optional, tag = "6")]
    pub volume_content_source: ::core::option::Option<VolumeContentSource>,
    /// Specifies where (regions, zones, racks, etc.) the provisioned
    /// volume MUST be accessible from.
    /// An SP SHALL advertise the requirements for topological
    /// accessibility information in documentation. COs SHALL only specify
    /// topological accessibility information supported by the SP.
    /// This field is OPTIONAL.
    /// This field SHALL NOT be specified unless the SP has the
    /// VOLUME_ACCESSIBILITY_CONSTRAINTS plugin capability.
    /// If this field is not specified and the SP has the
    /// VOLUME_ACCESSIBILITY_CONSTRAINTS plugin capability, the SP MAY
    /// choose where the provisioned volume is accessible from.
    #[prost(message, optional, tag = "7")]
    pub accessibility_requirements: ::core::option::Option<TopologyRequirement>,
    /// Plugins MUST treat these
    /// as if they take precedence over the parameters field.
    /// This field SHALL NOT be specified unless the SP has the
    /// MODIFY_VOLUME plugin capability.
    #[prost(map = "string, string", tag = "8")]
    pub mutable_parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Specifies what source the volume will be created from. One of the
/// type fields MUST be specified.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct VolumeContentSource {
    #[prost(oneof = "volume_content_source::Type", tags = "1, 2")]
    pub r#type: ::core::option::Option<volume_content_source::Type>,
}
/// Nested message and enum types in `VolumeContentSource`.
pub mod volume_content_source {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct SnapshotSource {
        /// Contains identity information for the existing source snapshot.
        /// This field is REQUIRED. Plugin is REQUIRED to support creating
        /// volume from snapshot if it supports the capability
        /// CREATE_DELETE_SNAPSHOT.
        #[prost(string, tag = "1")]
        pub snapshot_id: ::prost::alloc::string::String,
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct VolumeSource {
        /// Contains identity information for the existing source volume.
        /// This field is REQUIRED. Plugins reporting CLONE_VOLUME
        /// capability MUST support creating a volume from another volume.
        #[prost(string, tag = "1")]
        pub volume_id: ::prost::alloc::string::String,
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Type {
        #[prost(message, tag = "1")]
        Snapshot(SnapshotSource),
        #[prost(message, tag = "2")]
        Volume(VolumeSource),
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateVolumeResponse {
    /// Contains all attributes of the newly created volume that are
    /// relevant to the CO along with information required by the Plugin
    /// to uniquely identify the volume. This field is REQUIRED.
    #[prost(message, optional, tag = "1")]
    pub volume: ::core::option::Option<Volume>,
}
/// Specify a capability of a volume.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct VolumeCapability {
    /// This is a REQUIRED field.
    #[prost(message, optional, tag = "3")]
    pub access_mode: ::core::option::Option<volume_capability::AccessMode>,
    /// Specifies what API the volume will be accessed using. One of the
    /// following fields MUST be specified.
    #[prost(oneof = "volume_capability::AccessType", tags = "1, 2")]
    pub access_type: ::core::option::Option<volume_capability::AccessType>,
}
/// Nested message and enum types in `VolumeCapability`.
pub mod volume_capability {
    /// Indicate that the volume will be accessed via the block device API.
    ///
    /// Intentionally empty, for now.
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct BlockVolume {}
    /// Indicate that the volume will be accessed via the filesystem API.
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct MountVolume {
        /// The filesystem type. This field is OPTIONAL.
        /// An empty string is equal to an unspecified field value.
        #[prost(string, tag = "1")]
        pub fs_type: ::prost::alloc::string::String,
        /// The mount options that can be used for the volume. This field is
        /// OPTIONAL. `mount_flags` MAY contain sensitive information.
        /// Therefore, the CO and the Plugin MUST NOT leak this information
        /// to untrusted entities. The total size of this repeated field
        /// SHALL NOT exceed 4 KiB.
        #[prost(string, repeated, tag = "2")]
        pub mount_flags: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
        /// If SP has VOLUME_MOUNT_GROUP node capability and CO provides
        /// this field then SP MUST ensure that the volume_mount_group
        /// parameter is passed as the group identifier to the underlying
        /// operating system mount system call, with the understanding
        /// that the set of available mount call parameters and/or
        /// mount implementations may vary across operating systems.
        /// Additionally, new file and/or directory entries written to
        /// the underlying filesystem SHOULD be permission-labeled in such a
        /// manner, unless otherwise modified by a workload, that they are
        /// both readable and writable by said mount group identifier.
        /// This is an OPTIONAL field.
        #[prost(string, tag = "3")]
        pub volume_mount_group: ::prost::alloc::string::String,
    }
    /// Specify how a volume can be accessed.
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct AccessMode {
        /// This field is REQUIRED.
        #[prost(enumeration = "access_mode::Mode", tag = "1")]
        pub mode: i32,
    }
    /// Nested message and enum types in `AccessMode`.
    pub mod access_mode {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Mode {
            Unknown = 0,
            /// Can only be published once as read/write on a single node, at
            /// any given time.
            SingleNodeWriter = 1,
            /// Can only be published once as readonly on a single node, at
            /// any given time.
            SingleNodeReaderOnly = 2,
            /// Can be published as readonly at multiple nodes simultaneously.
            MultiNodeReaderOnly = 3,
            /// Can be published at multiple nodes simultaneously. Only one of
            /// the node can be used as read/write. The rest will be readonly.
            MultiNodeSingleWriter = 4,
            /// Can be published as read/write at multiple nodes
            /// simultaneously.
            MultiNodeMultiWriter = 5,
            /// Can only be published once as read/write at a single workload
            /// on a single node, at any given time. SHOULD be used instead of
            /// SINGLE_NODE_WRITER for COs using the experimental
            /// SINGLE_NODE_MULTI_WRITER capability.
            SingleNodeSingleWriter = 6,
            /// Can be published as read/write at multiple workloads on a
            /// single node simultaneously. SHOULD be used instead of
            /// SINGLE_NODE_WRITER for COs using the experimental
            /// SINGLE_NODE_MULTI_WRITER capability.
            SingleNodeMultiWriter = 7,
        }
        impl Mode {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Mode::Unknown => "UNKNOWN",
                    Mode::SingleNodeWriter => "SINGLE_NODE_WRITER",
                    Mode::SingleNodeReaderOnly => "SINGLE_NODE_READER_ONLY",
                    Mode::MultiNodeReaderOnly => "MULTI_NODE_READER_ONLY",
                    Mode::MultiNodeSingleWriter => "MULTI_NODE_SINGLE_WRITER",
                    Mode::MultiNodeMultiWriter => "MULTI_NODE_MULTI_WRITER",
                    Mode::SingleNodeSingleWriter => "SINGLE_NODE_SINGLE_WRITER",
                    Mode::SingleNodeMultiWriter => "SINGLE_NODE_MULTI_WRITER",
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "SINGLE_NODE_WRITER" => Some(Self::SingleNodeWriter),
                    "SINGLE_NODE_READER_ONLY" => Some(Self::SingleNodeReaderOnly),
                    "MULTI_NODE_READER_ONLY" => Some(Self::MultiNodeReaderOnly),
                    "MULTI_NODE_SINGLE_WRITER" => Some(Self::MultiNodeSingleWriter),
                    "MULTI_NODE_MULTI_WRITER" => Some(Self::MultiNodeMultiWriter),
                    "SINGLE_NODE_SINGLE_WRITER" => Some(Self::SingleNodeSingleWriter),
                    "SINGLE_NODE_MULTI_WRITER" => Some(Self::SingleNodeMultiWriter),
                    _ => None,
                }
            }
        }
    }
    /// Specifies what API the volume will be accessed using. One of the
    /// following fields MUST be specified.
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum AccessType {
        #[prost(message, tag = "1")]
        Block(BlockVolume),
        #[prost(message, tag = "2")]
        Mount(MountVolume),
    }
}
/// The capacity of the storage space in bytes. To specify an exact size,
/// `required_bytes` and `limit_bytes` SHALL be set to the same value. At
/// least one of the these fields MUST be specified.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CapacityRange {
    /// Volume MUST be at least this big. This field is OPTIONAL.
    /// A value of 0 is equal to an unspecified field value.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "1")]
    pub required_bytes: i64,
    /// Volume MUST not be bigger than this. This field is OPTIONAL.
    /// A value of 0 is equal to an unspecified field value.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "2")]
    pub limit_bytes: i64,
}
/// Information about a specific volume.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct Volume {
    /// The capacity of the volume in bytes. This field is OPTIONAL. If not
    /// set (value of 0), it indicates that the capacity of the volume is
    /// unknown (e.g., NFS share).
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "1")]
    pub capacity_bytes: i64,
    /// The identifier for this volume, generated by the plugin.
    /// This field is REQUIRED.
    /// This field MUST contain enough information to uniquely identify
    /// this specific volume vs all other volumes supported by this plugin.
    /// This field SHALL be used by the CO in subsequent calls to refer to
    /// this volume.
    /// The SP is NOT responsible for global uniqueness of volume_id across
    /// multiple SPs.
    #[prost(string, tag = "2")]
    pub volume_id: ::prost::alloc::string::String,
    /// Opaque static properties of the volume. SP MAY use this field to
    /// ensure subsequent volume validation and publishing calls have
    /// contextual information.
    /// The contents of this field SHALL be opaque to a CO.
    /// The contents of this field SHALL NOT be mutable.
    /// The contents of this field SHALL be safe for the CO to cache.
    /// The contents of this field SHOULD NOT contain sensitive
    /// information.
    /// The contents of this field SHOULD NOT be used for uniquely
    /// identifying a volume. The `volume_id` alone SHOULD be sufficient to
    /// identify the volume.
    /// A volume uniquely identified by `volume_id` SHALL always report the
    /// same volume_context.
    /// This field is OPTIONAL and when present MUST be passed to volume
    /// validation and publishing calls.
    #[prost(map = "string, string", tag = "3")]
    pub volume_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// If specified, indicates that the volume is not empty and is
    /// pre-populated with data from the specified source.
    /// This field is OPTIONAL.
    #[prost(message, optional, tag = "4")]
    pub content_source: ::core::option::Option<VolumeContentSource>,
    /// Specifies where (regions, zones, racks, etc.) the provisioned
    /// volume is accessible from.
    /// A plugin that returns this field MUST also set the
    /// VOLUME_ACCESSIBILITY_CONSTRAINTS plugin capability.
    /// An SP MAY specify multiple topologies to indicate the volume is
    /// accessible from multiple locations.
    /// COs MAY use this information along with the topology information
    /// returned by NodeGetInfo to ensure that a given volume is accessible
    /// from a given node when scheduling workloads.
    /// This field is OPTIONAL. If it is not specified, the CO MAY assume
    /// the volume is equally accessible from all nodes in the cluster and
    /// MAY schedule workloads referencing the volume on any available
    /// node.
    ///
    /// Example 1:
    ///    accessible_topology = {"region": "R1", "zone": "Z2"}
    /// Indicates a volume accessible only from the "region" "R1" and the
    /// "zone" "Z2".
    ///
    /// Example 2:
    ///    accessible_topology =
    ///      {"region": "R1", "zone": "Z2"},
    ///      {"region": "R1", "zone": "Z3"}
    /// Indicates a volume accessible from both "zone" "Z2" and "zone" "Z3"
    /// in the "region" "R1".
    #[prost(message, repeated, tag = "5")]
    pub accessible_topology: ::prost::alloc::vec::Vec<Topology>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct TopologyRequirement {
    /// Specifies the list of topologies the provisioned volume MUST be
    /// accessible from.
    /// This field is OPTIONAL. If TopologyRequirement is specified either
    /// requisite or preferred or both MUST be specified.
    ///
    /// If requisite is specified, the provisioned volume MUST be
    /// accessible from at least one of the requisite topologies.
    ///
    /// Given
    ///    x = number of topologies provisioned volume is accessible from
    ///    n = number of requisite topologies
    /// The CO MUST ensure n >= 1. The SP MUST ensure x >= 1
    /// If x==n, then the SP MUST make the provisioned volume available to
    /// all topologies from the list of requisite topologies. If it is
    /// unable to do so, the SP MUST fail the CreateVolume call.
    /// For example, if a volume should be accessible from a single zone,
    /// and requisite =
    ///    {"region": "R1", "zone": "Z2"}
    /// then the provisioned volume MUST be accessible from the "region"
    /// "R1" and the "zone" "Z2".
    /// Similarly, if a volume should be accessible from two zones, and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"}
    /// then the provisioned volume MUST be accessible from the "region"
    /// "R1" and both "zone" "Z2" and "zone" "Z3".
    ///
    /// If x<n, then the SP SHALL choose x unique topologies from the list
    /// of requisite topologies. If it is unable to do so, the SP MUST fail
    /// the CreateVolume call.
    /// For example, if a volume should be accessible from a single zone,
    /// and requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"}
    /// then the SP may choose to make the provisioned volume available in
    /// either the "zone" "Z2" or the "zone" "Z3" in the "region" "R1".
    /// Similarly, if a volume should be accessible from two zones, and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"},
    ///    {"region": "R1", "zone": "Z4"}
    /// then the provisioned volume MUST be accessible from any combination
    /// of two unique topologies: e.g. "R1/Z2" and "R1/Z3", or "R1/Z2" and
    ///   "R1/Z4", or "R1/Z3" and "R1/Z4".
    ///
    /// If x>n, then the SP MUST make the provisioned volume available from
    /// all topologies from the list of requisite topologies and MAY choose
    /// the remaining x-n unique topologies from the list of all possible
    /// topologies. If it is unable to do so, the SP MUST fail the
    /// CreateVolume call.
    /// For example, if a volume should be accessible from two zones, and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"}
    /// then the provisioned volume MUST be accessible from the "region"
    /// "R1" and the "zone" "Z2" and the SP may select the second zone
    /// independently, e.g. "R1/Z4".
    #[prost(message, repeated, tag = "1")]
    pub requisite: ::prost::alloc::vec::Vec<Topology>,
    /// Specifies the list of topologies the CO would prefer the volume to
    /// be provisioned in.
    ///
    /// This field is OPTIONAL. If TopologyRequirement is specified either
    /// requisite or preferred or both MUST be specified.
    ///
    /// An SP MUST attempt to make the provisioned volume available using
    /// the preferred topologies in order from first to last.
    ///
    /// If requisite is specified, all topologies in preferred list MUST
    /// also be present in the list of requisite topologies.
    ///
    /// If the SP is unable to to make the provisioned volume available
    /// from any of the preferred topologies, the SP MAY choose a topology
    /// from the list of requisite topologies.
    /// If the list of requisite topologies is not specified, then the SP
    /// MAY choose from the list of all possible topologies.
    /// If the list of requisite topologies is specified and the SP is
    /// unable to to make the provisioned volume available from any of the
    /// requisite topologies it MUST fail the CreateVolume call.
    ///
    /// Example 1:
    /// Given a volume should be accessible from a single zone, and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"}
    /// preferred =
    ///    {"region": "R1", "zone": "Z3"}
    /// then the SP SHOULD first attempt to make the provisioned volume
    /// available from "zone" "Z3" in the "region" "R1" and fall back to
    /// "zone" "Z2" in the "region" "R1" if that is not possible.
    ///
    /// Example 2:
    /// Given a volume should be accessible from a single zone, and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"},
    ///    {"region": "R1", "zone": "Z4"},
    ///    {"region": "R1", "zone": "Z5"}
    /// preferred =
    ///    {"region": "R1", "zone": "Z4"},
    ///    {"region": "R1", "zone": "Z2"}
    /// then the SP SHOULD first attempt to make the provisioned volume
    /// accessible from "zone" "Z4" in the "region" "R1" and fall back to
    /// "zone" "Z2" in the "region" "R1" if that is not possible. If that
    /// is not possible, the SP may choose between either the "zone"
    /// "Z3" or "Z5" in the "region" "R1".
    ///
    /// Example 3:
    /// Given a volume should be accessible from TWO zones (because an
    /// opaque parameter in CreateVolumeRequest, for example, specifies
    /// the volume is accessible from two zones, aka synchronously
    /// replicated), and
    /// requisite =
    ///    {"region": "R1", "zone": "Z2"},
    ///    {"region": "R1", "zone": "Z3"},
    ///    {"region": "R1", "zone": "Z4"},
    ///    {"region": "R1", "zone": "Z5"}
    /// preferred =
    ///    {"region": "R1", "zone": "Z5"},
    ///    {"region": "R1", "zone": "Z3"}
    /// then the SP SHOULD first attempt to make the provisioned volume
    /// accessible from the combination of the two "zones" "Z5" and "Z3" in
    /// the "region" "R1". If that's not possible, it should fall back to
    /// a combination of "Z5" and other possibilities from the list of
    /// requisite. If that's not possible, it should fall back  to a
    /// combination of "Z3" and other possibilities from the list of
    /// requisite. If that's not possible, it should fall back  to a
    /// combination of other possibilities from the list of requisite.
    #[prost(message, repeated, tag = "2")]
    pub preferred: ::prost::alloc::vec::Vec<Topology>,
}
/// Topology is a map of topological domains to topological segments.
/// A topological domain is a sub-division of a cluster, like "region",
/// "zone", "rack", etc.
/// A topological segment is a specific instance of a topological domain,
/// like "zone3", "rack3", etc.
/// For example {"com.company/zone": "Z1", "com.company/rack": "R3"}
/// Valid keys have two segments: an OPTIONAL prefix and name, separated
/// by a slash (/), for example: "com.company.example/zone".
/// The key name segment is REQUIRED. The prefix is OPTIONAL.
/// The key name MUST be 63 characters or less, begin and end with an
/// alphanumeric character (\[a-z0-9A-Z\]), and contain only dashes (-),
/// underscores (_), dots (.), or alphanumerics in between, for example
/// "zone".
/// The key prefix MUST be 63 characters or less, begin and end with a
/// lower-case alphanumeric character (\[a-z0-9\]), contain only
/// dashes (-), dots (.), or lower-case alphanumerics in between, and
/// follow domain name notation format
/// (<https://tools.ietf.org/html/rfc1035#section-2.3.1>).
/// The key prefix SHOULD include the plugin's host company name and/or
/// the plugin name, to minimize the possibility of collisions with keys
/// from other plugins.
/// If a key prefix is specified, it MUST be identical across all
/// topology keys returned by the SP (across all RPCs).
/// Keys MUST be case-insensitive. Meaning the keys "Zone" and "zone"
/// MUST not both exist.
/// Each value (topological segment) MUST contain 1 or more strings.
/// Each string MUST be 63 characters or less and begin and end with an
/// alphanumeric character with '-', '_', '.', or alphanumerics in
/// between.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct Topology {
    #[prost(map = "string, string", tag = "1")]
    pub segments: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteVolumeRequest {
    /// The ID of the volume to be deprovisioned.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete volume deletion request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "2")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerPublishVolumeRequest {
    /// The ID of the volume to be used on a node.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The ID of the node. This field is REQUIRED. The CO SHALL set this
    /// field to match the node ID returned by `NodeGetInfo`.
    #[prost(string, tag = "2")]
    pub node_id: ::prost::alloc::string::String,
    /// Volume capability describing how the CO intends to use this volume.
    /// SP MUST ensure the CO can use the published volume as described.
    /// Otherwise SP MUST return the appropriate gRPC error code.
    /// This is a REQUIRED field.
    #[prost(message, optional, tag = "3")]
    pub volume_capability: ::core::option::Option<VolumeCapability>,
    /// Indicates SP MUST publish the volume in readonly mode.
    /// CO MUST set this field to false if SP does not have the
    /// PUBLISH_READONLY controller capability.
    /// This is a REQUIRED field.
    #[prost(bool, tag = "4")]
    pub readonly: bool,
    /// Secrets required by plugin to complete controller publish volume
    /// request. This field is OPTIONAL. Refer to the
    /// `Secrets Requirements` section on how to use this field.
    #[prost(map = "string, string", tag = "5")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Volume context as returned by SP in
    /// CreateVolumeResponse.Volume.volume_context.
    /// This field is OPTIONAL and MUST match the volume_context of the
    /// volume identified by `volume_id`.
    #[prost(map = "string, string", tag = "6")]
    pub volume_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerPublishVolumeResponse {
    /// Opaque static publish properties of the volume. SP MAY use this
    /// field to ensure subsequent `NodeStageVolume` or `NodePublishVolume`
    /// calls calls have contextual information.
    /// The contents of this field SHALL be opaque to a CO.
    /// The contents of this field SHALL NOT be mutable.
    /// The contents of this field SHALL be safe for the CO to cache.
    /// The contents of this field SHOULD NOT contain sensitive
    /// information.
    /// The contents of this field SHOULD NOT be used for uniquely
    /// identifying a volume. The `volume_id` alone SHOULD be sufficient to
    /// identify the volume.
    /// This field is OPTIONAL and when present MUST be passed to
    /// subsequent `NodeStageVolume` or `NodePublishVolume` calls
    #[prost(map = "string, string", tag = "1")]
    pub publish_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerUnpublishVolumeRequest {
    /// The ID of the volume. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The ID of the node. This field is OPTIONAL. The CO SHOULD set this
    /// field to match the node ID returned by `NodeGetInfo` or leave it
    /// unset. If the value is set, the SP MUST unpublish the volume from
    /// the specified node. If the value is unset, the SP MUST unpublish
    /// the volume from all nodes it is published to.
    #[prost(string, tag = "2")]
    pub node_id: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete controller unpublish volume
    /// request. This SHOULD be the same secrets passed to the
    /// ControllerPublishVolume call for the specified volume.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerUnpublishVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ValidateVolumeCapabilitiesRequest {
    /// The ID of the volume to check. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// Volume context as returned by SP in
    /// CreateVolumeResponse.Volume.volume_context.
    /// This field is OPTIONAL and MUST match the volume_context of the
    /// volume identified by `volume_id`.
    #[prost(map = "string, string", tag = "2")]
    pub volume_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// The capabilities that the CO wants to check for the volume. This
    /// call SHALL return "confirmed" only if all the volume capabilities
    /// specified below are supported. This field is REQUIRED.
    #[prost(message, repeated, tag = "3")]
    pub volume_capabilities: ::prost::alloc::vec::Vec<VolumeCapability>,
    /// See CreateVolumeRequest.parameters.
    /// This field is OPTIONAL.
    #[prost(map = "string, string", tag = "4")]
    pub parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Secrets required by plugin to complete volume validation request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "5")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// See CreateVolumeRequest.mutable_parameters.
    /// This field is OPTIONAL.
    #[prost(map = "string, string", tag = "6")]
    pub mutable_parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ValidateVolumeCapabilitiesResponse {
    /// Confirmed indicates to the CO the set of capabilities that the
    /// plugin has validated. This field SHALL only be set to a non-empty
    /// value for successful validation responses.
    /// For successful validation responses, the CO SHALL compare the
    /// fields of this message to the originally requested capabilities in
    /// order to guard against an older plugin reporting "valid" for newer
    /// capability fields that it does not yet understand.
    /// This field is OPTIONAL.
    #[prost(message, optional, tag = "1")]
    pub confirmed: ::core::option::Option<
        validate_volume_capabilities_response::Confirmed,
    >,
    /// Message to the CO if `confirmed` above is empty. This field is
    /// OPTIONAL.
    /// An empty string is equal to an unspecified field value.
    #[prost(string, tag = "2")]
    pub message: ::prost::alloc::string::String,
}
/// Nested message and enum types in `ValidateVolumeCapabilitiesResponse`.
pub mod validate_volume_capabilities_response {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Confirmed {
        /// Volume context validated by the plugin.
        /// This field is OPTIONAL.
        #[prost(map = "string, string", tag = "1")]
        pub volume_context: ::std::collections::HashMap<
            ::prost::alloc::string::String,
            ::prost::alloc::string::String,
        >,
        /// Volume capabilities supported by the plugin.
        /// This field is REQUIRED.
        #[prost(message, repeated, tag = "2")]
        pub volume_capabilities: ::prost::alloc::vec::Vec<super::VolumeCapability>,
        /// The volume creation parameters validated by the plugin.
        /// This field is OPTIONAL.
        #[prost(map = "string, string", tag = "3")]
        pub parameters: ::std::collections::HashMap<
            ::prost::alloc::string::String,
            ::prost::alloc::string::String,
        >,
        /// The volume creation mutable_parameters validated by the plugin.
        /// This field is OPTIONAL.
        #[prost(map = "string, string", tag = "4")]
        pub mutable_parameters: ::std::collections::HashMap<
            ::prost::alloc::string::String,
            ::prost::alloc::string::String,
        >,
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ListVolumesRequest {
    /// If specified (non-zero value), the Plugin MUST NOT return more
    /// entries than this number in the response. If the actual number of
    /// entries is more than this number, the Plugin MUST set `next_token`
    /// in the response which can be used to get the next page of entries
    /// in the subsequent `ListVolumes` call. This field is OPTIONAL. If
    /// not specified (zero value), it means there is no restriction on the
    /// number of entries that can be returned.
    /// The value of this field MUST NOT be negative.
    #[prost(int32, tag = "1")]
    pub max_entries: i32,
    /// A token to specify where to start paginating. Set this field to
    /// `next_token` returned by a previous `ListVolumes` call to get the
    /// next page of entries. This field is OPTIONAL.
    /// An empty string is equal to an unspecified field value.
    #[prost(string, tag = "2")]
    pub starting_token: ::prost::alloc::string::String,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ListVolumesResponse {
    #[prost(message, repeated, tag = "1")]
    pub entries: ::prost::alloc::vec::Vec<list_volumes_response::Entry>,
    /// This token allows you to get the next page of entries for
    /// `ListVolumes` request. If the number of entries is larger than
    /// `max_entries`, use the `next_token` as a value for the
    /// `starting_token` field in the next `ListVolumes` request. This
    /// field is OPTIONAL.
    /// An empty string is equal to an unspecified field value.
    #[prost(string, tag = "2")]
    pub next_token: ::prost::alloc::string::String,
}
/// Nested message and enum types in `ListVolumesResponse`.
pub mod list_volumes_response {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct VolumeStatus {
        /// A list of all `node_id` of nodes that the volume in this entry
        /// is controller published on.
        /// This field is OPTIONAL. If it is not specified and the SP has
        /// the LIST_VOLUMES_PUBLISHED_NODES controller capability, the CO
        /// MAY assume the volume is not controller published to any nodes.
        /// If the field is not specified and the SP does not have the
        /// LIST_VOLUMES_PUBLISHED_NODES controller capability, the CO MUST
        /// not interpret this field.
        /// published_node_ids MAY include nodes not published to or
        /// reported by the SP. The CO MUST be resilient to that.
        #[prost(string, repeated, tag = "1")]
        pub published_node_ids: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
        /// Information about the current condition of the volume.
        /// This field is OPTIONAL.
        /// This field MUST be specified if the
        /// VOLUME_CONDITION controller capability is supported.
        #[prost(message, optional, tag = "2")]
        pub volume_condition: ::core::option::Option<super::VolumeCondition>,
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Entry {
        /// This field is REQUIRED
        #[prost(message, optional, tag = "1")]
        pub volume: ::core::option::Option<super::Volume>,
        /// This field is OPTIONAL. This field MUST be specified if the
        /// LIST_VOLUMES_PUBLISHED_NODES controller capability is
        /// supported.
        #[prost(message, optional, tag = "2")]
        pub status: ::core::option::Option<VolumeStatus>,
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerGetVolumeRequest {
    /// The ID of the volume to fetch current volume information for.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerGetVolumeResponse {
    /// This field is REQUIRED
    #[prost(message, optional, tag = "1")]
    pub volume: ::core::option::Option<Volume>,
    /// This field is REQUIRED.
    #[prost(message, optional, tag = "2")]
    pub status: ::core::option::Option<controller_get_volume_response::VolumeStatus>,
}
/// Nested message and enum types in `ControllerGetVolumeResponse`.
pub mod controller_get_volume_response {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct VolumeStatus {
        /// A list of all the `node_id` of nodes that this volume is
        /// controller published on.
        /// This field is OPTIONAL.
        /// This field MUST be specified if the LIST_VOLUMES_PUBLISHED_NODES
        /// controller capability is supported.
        /// published_node_ids MAY include nodes not published to or
        /// reported by the SP. The CO MUST be resilient to that.
        #[prost(string, repeated, tag = "1")]
        pub published_node_ids: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
        /// Information about the current condition of the volume.
        /// This field is OPTIONAL.
        /// This field MUST be specified if the
        /// VOLUME_CONDITION controller capability is supported.
        #[prost(message, optional, tag = "2")]
        pub volume_condition: ::core::option::Option<super::VolumeCondition>,
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerModifyVolumeRequest {
    /// Contains identity information for the existing volume.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete modify volume request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "2")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Plugin specific volume attributes to mutate, passed in as
    /// opaque key-value pairs.
    /// This field is REQUIRED. The Plugin is responsible for
    /// parsing and validating these parameters. COs will treat these
    /// as opaque. The CO SHOULD specify the intended values of all mutable
    /// parameters it intends to modify. SPs MUST NOT modify volumes based
    /// on the absence of keys, only keys that are specified should result
    /// in modifications to the volume.
    #[prost(map = "string, string", tag = "3")]
    pub mutable_parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerModifyVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetCapacityRequest {
    /// If specified, the Plugin SHALL report the capacity of the storage
    /// that can be used to provision volumes that satisfy ALL of the
    /// specified `volume_capabilities`. These are the same
    /// `volume_capabilities` the CO will use in `CreateVolumeRequest`.
    /// This field is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub volume_capabilities: ::prost::alloc::vec::Vec<VolumeCapability>,
    /// If specified, the Plugin SHALL report the capacity of the storage
    /// that can be used to provision volumes with the given Plugin
    /// specific `parameters`. These are the same `parameters` the CO will
    /// use in `CreateVolumeRequest`. This field is OPTIONAL.
    #[prost(map = "string, string", tag = "2")]
    pub parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// If specified, the Plugin SHALL report the capacity of the storage
    /// that can be used to provision volumes that in the specified
    /// `accessible_topology`. This is the same as the
    /// `accessible_topology` the CO returns in a `CreateVolumeResponse`.
    /// This field is OPTIONAL. This field SHALL NOT be set unless the
    /// plugin advertises the VOLUME_ACCESSIBILITY_CONSTRAINTS capability.
    #[prost(message, optional, tag = "3")]
    pub accessible_topology: ::core::option::Option<Topology>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetCapacityResponse {
    /// The available capacity, in bytes, of the storage that can be used
    /// to provision volumes. If `volume_capabilities` or `parameters` is
    /// specified in the request, the Plugin SHALL take those into
    /// consideration when calculating the available capacity of the
    /// storage. This field is REQUIRED.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "1")]
    pub available_capacity: i64,
    /// The largest size that may be used in a
    /// CreateVolumeRequest.capacity_range.required_bytes field
    /// to create a volume with the same parameters as those in
    /// GetCapacityRequest.
    ///
    /// If `volume_capabilities` or `parameters` is
    /// specified in the request, the Plugin SHALL take those into
    /// consideration when calculating the minimum volume size of the
    /// storage.
    ///
    /// This field is OPTIONAL. MUST NOT be negative.
    /// The Plugin SHOULD provide a value for this field if it has
    /// a maximum size for individual volumes and leave it unset
    /// otherwise. COs MAY use it to make decision about
    /// where to create volumes.
    #[prost(message, optional, tag = "2")]
    pub maximum_volume_size: ::core::option::Option<i64>,
    /// The smallest size that may be used in a
    /// CreateVolumeRequest.capacity_range.limit_bytes field
    /// to create a volume with the same parameters as those in
    /// GetCapacityRequest.
    ///
    /// If `volume_capabilities` or `parameters` is
    /// specified in the request, the Plugin SHALL take those into
    /// consideration when calculating the maximum volume size of the
    /// storage.
    ///
    /// This field is OPTIONAL. MUST NOT be negative.
    /// The Plugin SHOULD provide a value for this field if it has
    /// a minimum size for individual volumes and leave it unset
    /// otherwise. COs MAY use it to make decision about
    /// where to create volumes.
    #[prost(message, optional, tag = "3")]
    pub minimum_volume_size: ::core::option::Option<i64>,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerGetCapabilitiesRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerGetCapabilitiesResponse {
    /// All the capabilities that the controller service supports. This
    /// field is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub capabilities: ::prost::alloc::vec::Vec<ControllerServiceCapability>,
}
/// Specifies a capability of the controller service.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerServiceCapability {
    #[prost(oneof = "controller_service_capability::Type", tags = "1")]
    pub r#type: ::core::option::Option<controller_service_capability::Type>,
}
/// Nested message and enum types in `ControllerServiceCapability`.
pub mod controller_service_capability {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Rpc {
        #[prost(enumeration = "rpc::Type", tag = "1")]
        pub r#type: i32,
    }
    /// Nested message and enum types in `RPC`.
    pub mod rpc {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Type {
            Unknown = 0,
            CreateDeleteVolume = 1,
            PublishUnpublishVolume = 2,
            ListVolumes = 3,
            GetCapacity = 4,
            /// Currently the only way to consume a snapshot is to create
            /// a volume from it. Therefore plugins supporting
            /// CREATE_DELETE_SNAPSHOT MUST support creating volume from
            /// snapshot.
            CreateDeleteSnapshot = 5,
            ListSnapshots = 6,
            /// Plugins supporting volume cloning at the storage level MAY
            /// report this capability. The source volume MUST be managed by
            /// the same plugin. Not all volume sources and parameters
            /// combinations MAY work.
            CloneVolume = 7,
            /// Indicates the SP supports ControllerPublishVolume.readonly
            /// field.
            PublishReadonly = 8,
            /// See VolumeExpansion for details.
            ExpandVolume = 9,
            /// Indicates the SP supports the
            /// ListVolumesResponse.entry.published_node_ids field and the
            /// ControllerGetVolumeResponse.published_node_ids field.
            /// The SP MUST also support PUBLISH_UNPUBLISH_VOLUME.
            ListVolumesPublishedNodes = 10,
            /// Indicates that the Controller service can report volume
            /// conditions.
            /// An SP MAY implement `VolumeCondition` in only the Controller
            /// Plugin, only the Node Plugin, or both.
            /// If `VolumeCondition` is implemented in both the Controller and
            /// Node Plugins, it SHALL report from different perspectives.
            /// If for some reason Controller and Node Plugins report
            /// misaligned volume conditions, CO SHALL assume the worst case
            /// is the truth.
            /// Note that, for alpha, `VolumeCondition` is intended be
            /// informative for humans only, not for automation.
            VolumeCondition = 11,
            /// Indicates the SP supports the ControllerGetVolume RPC.
            /// This enables COs to, for example, fetch per volume
            /// condition after a volume is provisioned.
            GetVolume = 12,
            /// Indicates the SP supports the SINGLE_NODE_SINGLE_WRITER and/or
            /// SINGLE_NODE_MULTI_WRITER access modes.
            /// These access modes are intended to replace the
            /// SINGLE_NODE_WRITER access mode to clarify the number of writers
            /// for a volume on a single node. Plugins MUST accept and allow
            /// use of the SINGLE_NODE_WRITER access mode when either
            /// SINGLE_NODE_SINGLE_WRITER and/or SINGLE_NODE_MULTI_WRITER are
            /// supported, in order to permit older COs to continue working.
            SingleNodeMultiWriter = 13,
            /// Indicates the SP supports modifying volume with mutable
            /// parameters. See ControllerModifyVolume for details.
            ModifyVolume = 14,
        }
        impl Type {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Type::Unknown => "UNKNOWN",
                    Type::CreateDeleteVolume => "CREATE_DELETE_VOLUME",
                    Type::PublishUnpublishVolume => "PUBLISH_UNPUBLISH_VOLUME",
                    Type::ListVolumes => "LIST_VOLUMES",
                    Type::GetCapacity => "GET_CAPACITY",
                    Type::CreateDeleteSnapshot => "CREATE_DELETE_SNAPSHOT",
                    Type::ListSnapshots => "LIST_SNAPSHOTS",
                    Type::CloneVolume => "CLONE_VOLUME",
                    Type::PublishReadonly => "PUBLISH_READONLY",
                    Type::ExpandVolume => "EXPAND_VOLUME",
                    Type::ListVolumesPublishedNodes => "LIST_VOLUMES_PUBLISHED_NODES",
                    Type::VolumeCondition => "VOLUME_CONDITION",
                    Type::GetVolume => "GET_VOLUME",
                    Type::SingleNodeMultiWriter => "SINGLE_NODE_MULTI_WRITER",
                    Type::ModifyVolume => "MODIFY_VOLUME",
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "CREATE_DELETE_VOLUME" => Some(Self::CreateDeleteVolume),
                    "PUBLISH_UNPUBLISH_VOLUME" => Some(Self::PublishUnpublishVolume),
                    "LIST_VOLUMES" => Some(Self::ListVolumes),
                    "GET_CAPACITY" => Some(Self::GetCapacity),
                    "CREATE_DELETE_SNAPSHOT" => Some(Self::CreateDeleteSnapshot),
                    "LIST_SNAPSHOTS" => Some(Self::ListSnapshots),
                    "CLONE_VOLUME" => Some(Self::CloneVolume),
                    "PUBLISH_READONLY" => Some(Self::PublishReadonly),
                    "EXPAND_VOLUME" => Some(Self::ExpandVolume),
                    "LIST_VOLUMES_PUBLISHED_NODES" => {
                        Some(Self::ListVolumesPublishedNodes)
                    }
                    "VOLUME_CONDITION" => Some(Self::VolumeCondition),
                    "GET_VOLUME" => Some(Self::GetVolume),
                    "SINGLE_NODE_MULTI_WRITER" => Some(Self::SingleNodeMultiWriter),
                    "MODIFY_VOLUME" => Some(Self::ModifyVolume),
                    _ => None,
                }
            }
        }
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Type {
        /// RPC that the controller supports.
        #[prost(message, tag = "1")]
        Rpc(Rpc),
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateSnapshotRequest {
    /// The ID of the source volume to be snapshotted.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub source_volume_id: ::prost::alloc::string::String,
    /// The suggested name for the snapshot. This field is REQUIRED for
    /// idempotency.
    /// Any Unicode string that conforms to the length limit is allowed
    /// except those containing the following banned characters:
    /// U+0000-U+0008, U+000B, U+000C, U+000E-U+001F, U+007F-U+009F.
    /// (These are control characters other than commonly used whitespace.)
    #[prost(string, tag = "2")]
    pub name: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete snapshot creation request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Plugin specific parameters passed in as opaque key-value pairs.
    /// This field is OPTIONAL. The Plugin is responsible for parsing and
    /// validating these parameters. COs will treat these as opaque.
    /// Use cases for opaque parameters:
    /// - Specify a policy to automatically clean up the snapshot.
    /// - Specify an expiration date for the snapshot.
    /// - Specify whether the snapshot is readonly or read/write.
    /// - Specify if the snapshot should be replicated to some place.
    /// - Specify primary or secondary for replication systems that
    ///    support snapshotting only on primary.
    #[prost(map = "string, string", tag = "4")]
    pub parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateSnapshotResponse {
    /// Contains all attributes of the newly created snapshot that are
    /// relevant to the CO along with information required by the Plugin
    /// to uniquely identify the snapshot. This field is REQUIRED.
    #[prost(message, optional, tag = "1")]
    pub snapshot: ::core::option::Option<Snapshot>,
}
/// Information about a specific snapshot.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct Snapshot {
    /// This is the complete size of the snapshot in bytes. The purpose of
    /// this field is to give CO guidance on how much space is needed to
    /// create a volume from this snapshot. The size of the volume MUST NOT
    /// be less than the size of the source snapshot. This field is
    /// OPTIONAL. If this field is not set, it indicates that this size is
    /// unknown. The value of this field MUST NOT be negative and a size of
    /// zero means it is unspecified.
    #[prost(int64, tag = "1")]
    pub size_bytes: i64,
    /// The identifier for this snapshot, generated by the plugin.
    /// This field is REQUIRED.
    /// This field MUST contain enough information to uniquely identify
    /// this specific snapshot vs all other snapshots supported by this
    /// plugin.
    /// This field SHALL be used by the CO in subsequent calls to refer to
    /// this snapshot.
    /// The SP is NOT responsible for global uniqueness of snapshot_id
    /// across multiple SPs.
    #[prost(string, tag = "2")]
    pub snapshot_id: ::prost::alloc::string::String,
    /// Identity information for the source volume. Note that creating a
    /// snapshot from a snapshot is not supported here so the source has to
    /// be a volume. This field is REQUIRED.
    #[prost(string, tag = "3")]
    pub source_volume_id: ::prost::alloc::string::String,
    /// Timestamp when the point-in-time snapshot is taken on the storage
    /// system. This field is REQUIRED.
    #[prost(message, optional, tag = "4")]
    pub creation_time: ::core::option::Option<::prost_types::Timestamp>,
    /// Indicates if a snapshot is ready to use as a
    /// `volume_content_source` in a `CreateVolumeRequest`. The default
    /// value is false. This field is REQUIRED.
    #[prost(bool, tag = "5")]
    pub ready_to_use: bool,
    /// The ID of the volume group snapshot that this snapshot is part of.
    /// It uniquely identifies the group snapshot on the storage system.
    /// This field is OPTIONAL.
    /// If this snapshot is a member of a volume group snapshot, and it
    /// MUST NOT be deleted as a stand alone snapshot, then the SP
    /// MUST provide the ID of the volume group snapshot in this field.
    /// If provided, CO MUST use this field in subsequent volume group
    /// snapshot operations to indicate that this snapshot is part of the
    /// specified group snapshot.
    /// If not provided, CO SHALL treat the snapshot as independent,
    /// and SP SHALL allow it to be deleted separately.
    /// If this message is inside a VolumeGroupSnapshot message, the value
    /// MUST be the same as the group_snapshot_id in that message.
    #[prost(string, tag = "6")]
    pub group_snapshot_id: ::prost::alloc::string::String,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteSnapshotRequest {
    /// The ID of the snapshot to be deleted.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub snapshot_id: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete snapshot deletion request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "2")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteSnapshotResponse {}
/// List all snapshots on the storage system regardless of how they were
/// created.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ListSnapshotsRequest {
    /// If specified (non-zero value), the Plugin MUST NOT return more
    /// entries than this number in the response. If the actual number of
    /// entries is more than this number, the Plugin MUST set `next_token`
    /// in the response which can be used to get the next page of entries
    /// in the subsequent `ListSnapshots` call. This field is OPTIONAL. If
    /// not specified (zero value), it means there is no restriction on the
    /// number of entries that can be returned.
    /// The value of this field MUST NOT be negative.
    #[prost(int32, tag = "1")]
    pub max_entries: i32,
    /// A token to specify where to start paginating. Set this field to
    /// `next_token` returned by a previous `ListSnapshots` call to get the
    /// next page of entries. This field is OPTIONAL.
    /// An empty string is equal to an unspecified field value.
    #[prost(string, tag = "2")]
    pub starting_token: ::prost::alloc::string::String,
    /// Identity information for the source volume. This field is OPTIONAL.
    /// It can be used to list snapshots by volume.
    #[prost(string, tag = "3")]
    pub source_volume_id: ::prost::alloc::string::String,
    /// Identity information for a specific snapshot. This field is
    /// OPTIONAL. It can be used to list only a specific snapshot.
    /// ListSnapshots will return with current snapshot information
    /// and will not block if the snapshot is being processed after
    /// it is cut.
    #[prost(string, tag = "4")]
    pub snapshot_id: ::prost::alloc::string::String,
    /// Secrets required by plugin to complete ListSnapshot request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "5")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ListSnapshotsResponse {
    #[prost(message, repeated, tag = "1")]
    pub entries: ::prost::alloc::vec::Vec<list_snapshots_response::Entry>,
    /// This token allows you to get the next page of entries for
    /// `ListSnapshots` request. If the number of entries is larger than
    /// `max_entries`, use the `next_token` as a value for the
    /// `starting_token` field in the next `ListSnapshots` request. This
    /// field is OPTIONAL.
    /// An empty string is equal to an unspecified field value.
    #[prost(string, tag = "2")]
    pub next_token: ::prost::alloc::string::String,
}
/// Nested message and enum types in `ListSnapshotsResponse`.
pub mod list_snapshots_response {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Entry {
        #[prost(message, optional, tag = "1")]
        pub snapshot: ::core::option::Option<super::Snapshot>,
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerExpandVolumeRequest {
    /// The ID of the volume to expand. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// This allows CO to specify the capacity requirements of the volume
    /// after expansion. This field is REQUIRED.
    #[prost(message, optional, tag = "2")]
    pub capacity_range: ::core::option::Option<CapacityRange>,
    /// Secrets required by the plugin for expanding the volume.
    /// This field is OPTIONAL.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Volume capability describing how the CO intends to use this volume.
    /// This allows SP to determine if volume is being used as a block
    /// device or mounted file system. For example - if volume is
    /// being used as a block device - the SP MAY set
    /// node_expansion_required to false in ControllerExpandVolumeResponse
    /// to skip invocation of NodeExpandVolume on the node by the CO.
    /// This is an OPTIONAL field.
    #[prost(message, optional, tag = "4")]
    pub volume_capability: ::core::option::Option<VolumeCapability>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ControllerExpandVolumeResponse {
    /// Capacity of volume after expansion. This field is REQUIRED.
    #[prost(int64, tag = "1")]
    pub capacity_bytes: i64,
    /// Whether node expansion is required for the volume. When true
    /// the CO MUST make NodeExpandVolume RPC call on the node. This field
    /// is REQUIRED.
    #[prost(bool, tag = "2")]
    pub node_expansion_required: bool,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeStageVolumeRequest {
    /// The ID of the volume to publish. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The CO SHALL set this field to the value returned by
    /// `ControllerPublishVolume` if the corresponding Controller Plugin
    /// has `PUBLISH_UNPUBLISH_VOLUME` controller capability, and SHALL be
    /// left unset if the corresponding Controller Plugin does not have
    /// this capability. This is an OPTIONAL field.
    #[prost(map = "string, string", tag = "2")]
    pub publish_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// The path to which the volume MAY be staged. It MUST be an
    /// absolute path in the root filesystem of the process serving this
    /// request, and MUST be a directory. The CO SHALL ensure that there
    /// is only one `staging_target_path` per volume. The CO SHALL ensure
    /// that the path is directory and that the process serving the
    /// request has `read` and `write` permission to that directory. The
    /// CO SHALL be responsible for creating the directory if it does not
    /// exist.
    /// This is a REQUIRED field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "3")]
    pub staging_target_path: ::prost::alloc::string::String,
    /// Volume capability describing how the CO intends to use this volume.
    /// SP MUST ensure the CO can use the staged volume as described.
    /// Otherwise SP MUST return the appropriate gRPC error code.
    /// This is a REQUIRED field.
    #[prost(message, optional, tag = "4")]
    pub volume_capability: ::core::option::Option<VolumeCapability>,
    /// Secrets required by plugin to complete node stage volume request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "5")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Volume context as returned by SP in
    /// CreateVolumeResponse.Volume.volume_context.
    /// This field is OPTIONAL and MUST match the volume_context of the
    /// volume identified by `volume_id`.
    #[prost(map = "string, string", tag = "6")]
    pub volume_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeStageVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeUnstageVolumeRequest {
    /// The ID of the volume. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The path at which the volume was staged. It MUST be an absolute
    /// path in the root filesystem of the process serving this request.
    /// This is a REQUIRED field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "2")]
    pub staging_target_path: ::prost::alloc::string::String,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeUnstageVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodePublishVolumeRequest {
    /// The ID of the volume to publish. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The CO SHALL set this field to the value returned by
    /// `ControllerPublishVolume` if the corresponding Controller Plugin
    /// has `PUBLISH_UNPUBLISH_VOLUME` controller capability, and SHALL be
    /// left unset if the corresponding Controller Plugin does not have
    /// this capability. This is an OPTIONAL field.
    #[prost(map = "string, string", tag = "2")]
    pub publish_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// The path to which the volume was staged by `NodeStageVolume`.
    /// It MUST be an absolute path in the root filesystem of the process
    /// serving this request.
    /// It MUST be set if the Node Plugin implements the
    /// `STAGE_UNSTAGE_VOLUME` node capability.
    /// This is an OPTIONAL field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "3")]
    pub staging_target_path: ::prost::alloc::string::String,
    /// The path to which the volume will be published. It MUST be an
    /// absolute path in the root filesystem of the process serving this
    /// request. The CO SHALL ensure uniqueness of target_path per volume.
    /// The CO SHALL ensure that the parent directory of this path exists
    /// and that the process serving the request has `read` and `write`
    /// permissions to that parent directory.
    /// For volumes with an access type of block, the SP SHALL place the
    /// block device at target_path.
    /// For volumes with an access type of mount, the SP SHALL place the
    /// mounted directory at target_path.
    /// Creation of target_path is the responsibility of the SP.
    /// This is a REQUIRED field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "4")]
    pub target_path: ::prost::alloc::string::String,
    /// Volume capability describing how the CO intends to use this volume.
    /// SP MUST ensure the CO can use the published volume as described.
    /// Otherwise SP MUST return the appropriate gRPC error code.
    /// This is a REQUIRED field.
    #[prost(message, optional, tag = "5")]
    pub volume_capability: ::core::option::Option<VolumeCapability>,
    /// Indicates SP MUST publish the volume in readonly mode.
    /// This field is REQUIRED.
    #[prost(bool, tag = "6")]
    pub readonly: bool,
    /// Secrets required by plugin to complete node publish volume request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "7")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Volume context as returned by SP in
    /// CreateVolumeResponse.Volume.volume_context.
    /// This field is OPTIONAL and MUST match the volume_context of the
    /// volume identified by `volume_id`.
    #[prost(map = "string, string", tag = "8")]
    pub volume_context: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodePublishVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeUnpublishVolumeRequest {
    /// The ID of the volume. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The path at which the volume was published. It MUST be an absolute
    /// path in the root filesystem of the process serving this request.
    /// The SP MUST delete the file or directory it created at this path.
    /// This is a REQUIRED field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "2")]
    pub target_path: ::prost::alloc::string::String,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeUnpublishVolumeResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetVolumeStatsRequest {
    /// The ID of the volume. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// It can be any valid path where volume was previously
    /// staged or published.
    /// It MUST be an absolute path in the root filesystem of
    /// the process serving this request.
    /// This is a REQUIRED field.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "2")]
    pub volume_path: ::prost::alloc::string::String,
    /// The path where the volume is staged, if the plugin has the
    /// STAGE_UNSTAGE_VOLUME capability, otherwise empty.
    /// If not empty, it MUST be an absolute path in the root
    /// filesystem of the process serving this request.
    /// This field is OPTIONAL.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "3")]
    pub staging_target_path: ::prost::alloc::string::String,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetVolumeStatsResponse {
    /// This field is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub usage: ::prost::alloc::vec::Vec<VolumeUsage>,
    /// Information about the current condition of the volume.
    /// This field is OPTIONAL.
    /// This field MUST be specified if the VOLUME_CONDITION node
    /// capability is supported.
    #[prost(message, optional, tag = "2")]
    pub volume_condition: ::core::option::Option<VolumeCondition>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct VolumeUsage {
    /// The available capacity in specified Unit. This field is OPTIONAL.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "1")]
    pub available: i64,
    /// The total capacity in specified Unit. This field is REQUIRED.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "2")]
    pub total: i64,
    /// The used capacity in specified Unit. This field is OPTIONAL.
    /// The value of this field MUST NOT be negative.
    #[prost(int64, tag = "3")]
    pub used: i64,
    /// Units by which values are measured. This field is REQUIRED.
    #[prost(enumeration = "volume_usage::Unit", tag = "4")]
    pub unit: i32,
}
/// Nested message and enum types in `VolumeUsage`.
pub mod volume_usage {
    #[derive(
        Clone,
        Copy,
        Debug,
        PartialEq,
        Eq,
        Hash,
        PartialOrd,
        Ord,
        ::prost::Enumeration
    )]
    #[repr(i32)]
    pub enum Unit {
        Unknown = 0,
        Bytes = 1,
        Inodes = 2,
    }
    impl Unit {
        /// String value of the enum field names used in the ProtoBuf definition.
        ///
        /// The values are not transformed in any way and thus are considered stable
        /// (if the ProtoBuf definition does not change) and safe for programmatic use.
        pub fn as_str_name(&self) -> &'static str {
            match self {
                Unit::Unknown => "UNKNOWN",
                Unit::Bytes => "BYTES",
                Unit::Inodes => "INODES",
            }
        }
        /// Creates an enum from field names used in the ProtoBuf definition.
        pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
            match value {
                "UNKNOWN" => Some(Self::Unknown),
                "BYTES" => Some(Self::Bytes),
                "INODES" => Some(Self::Inodes),
                _ => None,
            }
        }
    }
}
/// VolumeCondition represents the current condition of a volume.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct VolumeCondition {
    /// Normal volumes are available for use and operating optimally.
    /// An abnormal volume does not meet these criteria.
    /// This field is REQUIRED.
    #[prost(bool, tag = "1")]
    pub abnormal: bool,
    /// The message describing the condition of the volume.
    /// This field is REQUIRED.
    #[prost(string, tag = "2")]
    pub message: ::prost::alloc::string::String,
}
/// Intentionally empty.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetCapabilitiesRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetCapabilitiesResponse {
    /// All the capabilities that the node service supports. This field
    /// is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub capabilities: ::prost::alloc::vec::Vec<NodeServiceCapability>,
}
/// Specifies a capability of the node service.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeServiceCapability {
    #[prost(oneof = "node_service_capability::Type", tags = "1")]
    pub r#type: ::core::option::Option<node_service_capability::Type>,
}
/// Nested message and enum types in `NodeServiceCapability`.
pub mod node_service_capability {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Rpc {
        #[prost(enumeration = "rpc::Type", tag = "1")]
        pub r#type: i32,
    }
    /// Nested message and enum types in `RPC`.
    pub mod rpc {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Type {
            Unknown = 0,
            StageUnstageVolume = 1,
            /// If Plugin implements GET_VOLUME_STATS capability
            /// then it MUST implement NodeGetVolumeStats RPC
            /// call for fetching volume statistics.
            GetVolumeStats = 2,
            /// See VolumeExpansion for details.
            ExpandVolume = 3,
            /// Indicates that the Node service can report volume conditions.
            /// An SP MAY implement `VolumeCondition` in only the Node
            /// Plugin, only the Controller Plugin, or both.
            /// If `VolumeCondition` is implemented in both the Node and
            /// Controller Plugins, it SHALL report from different
            /// perspectives.
            /// If for some reason Node and Controller Plugins report
            /// misaligned volume conditions, CO SHALL assume the worst case
            /// is the truth.
            /// Note that, for alpha, `VolumeCondition` is intended to be
            /// informative for humans only, not for automation.
            VolumeCondition = 4,
            /// Indicates the SP supports the SINGLE_NODE_SINGLE_WRITER and/or
            /// SINGLE_NODE_MULTI_WRITER access modes.
            /// These access modes are intended to replace the
            /// SINGLE_NODE_WRITER access mode to clarify the number of writers
            /// for a volume on a single node. Plugins MUST accept and allow
            /// use of the SINGLE_NODE_WRITER access mode (subject to the
            /// processing rules for NodePublishVolume), when either
            /// SINGLE_NODE_SINGLE_WRITER and/or SINGLE_NODE_MULTI_WRITER are
            /// supported, in order to permit older COs to continue working.
            SingleNodeMultiWriter = 5,
            /// Indicates that Node service supports mounting volumes
            /// with provided volume group identifier during node stage
            /// or node publish RPC calls.
            VolumeMountGroup = 6,
        }
        impl Type {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Type::Unknown => "UNKNOWN",
                    Type::StageUnstageVolume => "STAGE_UNSTAGE_VOLUME",
                    Type::GetVolumeStats => "GET_VOLUME_STATS",
                    Type::ExpandVolume => "EXPAND_VOLUME",
                    Type::VolumeCondition => "VOLUME_CONDITION",
                    Type::SingleNodeMultiWriter => "SINGLE_NODE_MULTI_WRITER",
                    Type::VolumeMountGroup => "VOLUME_MOUNT_GROUP",
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "STAGE_UNSTAGE_VOLUME" => Some(Self::StageUnstageVolume),
                    "GET_VOLUME_STATS" => Some(Self::GetVolumeStats),
                    "EXPAND_VOLUME" => Some(Self::ExpandVolume),
                    "VOLUME_CONDITION" => Some(Self::VolumeCondition),
                    "SINGLE_NODE_MULTI_WRITER" => Some(Self::SingleNodeMultiWriter),
                    "VOLUME_MOUNT_GROUP" => Some(Self::VolumeMountGroup),
                    _ => None,
                }
            }
        }
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Type {
        /// RPC that the controller supports.
        #[prost(message, tag = "1")]
        Rpc(Rpc),
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetInfoRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeGetInfoResponse {
    /// The identifier of the node as understood by the SP.
    /// This field is REQUIRED.
    /// This field MUST contain enough information to uniquely identify
    /// this specific node vs all other nodes supported by this plugin.
    /// This field SHALL be used by the CO in subsequent calls, including
    /// `ControllerPublishVolume`, to refer to this node.
    /// The SP is NOT responsible for global uniqueness of node_id across
    /// multiple SPs.
    /// This field overrides the general CSI size limit.
    /// The size of this field SHALL NOT exceed 256 bytes. The general
    /// CSI size limit, 128 byte, is RECOMMENDED for best backwards
    /// compatibility.
    #[prost(string, tag = "1")]
    pub node_id: ::prost::alloc::string::String,
    /// Maximum number of volumes that controller can publish to the node.
    /// If value is not set or zero CO SHALL decide how many volumes of
    /// this type can be published by the controller to the node. The
    /// plugin MUST NOT set negative values here.
    /// This field is OPTIONAL.
    #[prost(int64, tag = "2")]
    pub max_volumes_per_node: i64,
    /// Specifies where (regions, zones, racks, etc.) the node is
    /// accessible from.
    /// A plugin that returns this field MUST also set the
    /// VOLUME_ACCESSIBILITY_CONSTRAINTS plugin capability.
    /// COs MAY use this information along with the topology information
    /// returned in CreateVolumeResponse to ensure that a given volume is
    /// accessible from a given node when scheduling workloads.
    /// This field is OPTIONAL. If it is not specified, the CO MAY assume
    /// the node is not subject to any topological constraint, and MAY
    /// schedule workloads that reference any volume V, such that there are
    /// no topological constraints declared for V.
    ///
    /// Example 1:
    ///    accessible_topology =
    ///      {"region": "R1", "zone": "Z2"}
    /// Indicates the node exists within the "region" "R1" and the "zone"
    /// "Z2".
    #[prost(message, optional, tag = "3")]
    pub accessible_topology: ::core::option::Option<Topology>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeExpandVolumeRequest {
    /// The ID of the volume. This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub volume_id: ::prost::alloc::string::String,
    /// The path on which volume is available. This field is REQUIRED.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "2")]
    pub volume_path: ::prost::alloc::string::String,
    /// This allows CO to specify the capacity requirements of the volume
    /// after expansion. If capacity_range is omitted then a plugin MAY
    /// inspect the file system of the volume to determine the maximum
    /// capacity to which the volume can be expanded. In such cases a
    /// plugin MAY expand the volume to its maximum capacity.
    /// This field is OPTIONAL.
    #[prost(message, optional, tag = "3")]
    pub capacity_range: ::core::option::Option<CapacityRange>,
    /// The path where the volume is staged, if the plugin has the
    /// STAGE_UNSTAGE_VOLUME capability, otherwise empty.
    /// If not empty, it MUST be an absolute path in the root
    /// filesystem of the process serving this request.
    /// This field is OPTIONAL.
    /// This field overrides the general CSI size limit.
    /// SP SHOULD support the maximum path length allowed by the operating
    /// system/filesystem, but, at a minimum, SP MUST accept a max path
    /// length of at least 128 bytes.
    #[prost(string, tag = "4")]
    pub staging_target_path: ::prost::alloc::string::String,
    /// Volume capability describing how the CO intends to use this volume.
    /// This allows SP to determine if volume is being used as a block
    /// device or mounted file system. For example - if volume is being
    /// used as a block device the SP MAY choose to skip expanding the
    /// filesystem in NodeExpandVolume implementation but still perform
    /// rest of the housekeeping needed for expanding the volume. If
    /// volume_capability is omitted the SP MAY determine
    /// access_type from given volume_path for the volume and perform
    /// node expansion. This is an OPTIONAL field.
    #[prost(message, optional, tag = "5")]
    pub volume_capability: ::core::option::Option<VolumeCapability>,
    /// Secrets required by plugin to complete node expand volume request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    #[prost(map = "string, string", tag = "6")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct NodeExpandVolumeResponse {
    /// The capacity of the volume in bytes. This field is OPTIONAL.
    #[prost(int64, tag = "1")]
    pub capacity_bytes: i64,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GroupControllerGetCapabilitiesRequest {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GroupControllerGetCapabilitiesResponse {
    /// All the capabilities that the group controller service supports.
    /// This field is OPTIONAL.
    #[prost(message, repeated, tag = "1")]
    pub capabilities: ::prost::alloc::vec::Vec<GroupControllerServiceCapability>,
}
/// Specifies a capability of the group controller service.
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GroupControllerServiceCapability {
    #[prost(oneof = "group_controller_service_capability::Type", tags = "1")]
    pub r#type: ::core::option::Option<group_controller_service_capability::Type>,
}
/// Nested message and enum types in `GroupControllerServiceCapability`.
pub mod group_controller_service_capability {
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Message)]
    pub struct Rpc {
        #[prost(enumeration = "rpc::Type", tag = "1")]
        pub r#type: i32,
    }
    /// Nested message and enum types in `RPC`.
    pub mod rpc {
        #[derive(
            Clone,
            Copy,
            Debug,
            PartialEq,
            Eq,
            Hash,
            PartialOrd,
            Ord,
            ::prost::Enumeration
        )]
        #[repr(i32)]
        pub enum Type {
            Unknown = 0,
            /// Indicates that the group controller plugin supports
            /// creating, deleting, and getting details of a volume
            /// group snapshot.
            CreateDeleteGetVolumeGroupSnapshot = 1,
        }
        impl Type {
            /// String value of the enum field names used in the ProtoBuf definition.
            ///
            /// The values are not transformed in any way and thus are considered stable
            /// (if the ProtoBuf definition does not change) and safe for programmatic use.
            pub fn as_str_name(&self) -> &'static str {
                match self {
                    Type::Unknown => "UNKNOWN",
                    Type::CreateDeleteGetVolumeGroupSnapshot => {
                        "CREATE_DELETE_GET_VOLUME_GROUP_SNAPSHOT"
                    }
                }
            }
            /// Creates an enum from field names used in the ProtoBuf definition.
            pub fn from_str_name(value: &str) -> ::core::option::Option<Self> {
                match value {
                    "UNKNOWN" => Some(Self::Unknown),
                    "CREATE_DELETE_GET_VOLUME_GROUP_SNAPSHOT" => {
                        Some(Self::CreateDeleteGetVolumeGroupSnapshot)
                    }
                    _ => None,
                }
            }
        }
    }
    #[allow(clippy::derive_partial_eq_without_eq)]
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Type {
        /// RPC that the controller supports.
        #[prost(message, tag = "1")]
        Rpc(Rpc),
    }
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateVolumeGroupSnapshotRequest {
    /// The suggested name for the group snapshot. This field is REQUIRED
    /// for idempotency.
    /// Any Unicode string that conforms to the length limit is allowed
    /// except those containing the following banned characters:
    /// U+0000-U+0008, U+000B, U+000C, U+000E-U+001F, U+007F-U+009F.
    /// (These are control characters other than commonly used whitespace.)
    #[prost(string, tag = "1")]
    pub name: ::prost::alloc::string::String,
    /// volume IDs of the source volumes to be snapshotted together.
    /// This field is REQUIRED.
    #[prost(string, repeated, tag = "2")]
    pub source_volume_ids: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
    /// Secrets required by plugin to complete
    /// ControllerCreateVolumeGroupSnapshot request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    /// The secrets provided in this field SHOULD be the same for
    /// all group snapshot operations on the same group snapshot.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
    /// Plugin specific parameters passed in as opaque key-value pairs.
    /// This field is OPTIONAL. The Plugin is responsible for parsing and
    /// validating these parameters. COs will treat these as opaque.
    #[prost(map = "string, string", tag = "4")]
    pub parameters: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CreateVolumeGroupSnapshotResponse {
    /// Contains all attributes of the newly created group snapshot.
    /// This field is REQUIRED.
    #[prost(message, optional, tag = "1")]
    pub group_snapshot: ::core::option::Option<VolumeGroupSnapshot>,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct VolumeGroupSnapshot {
    /// The identifier for this group snapshot, generated by the plugin.
    /// This field MUST contain enough information to uniquely identify
    /// this specific snapshot vs all other group snapshots supported by
    /// this plugin.
    /// This field SHALL be used by the CO in subsequent calls to refer to
    /// this group snapshot.
    /// The SP is NOT responsible for global uniqueness of
    /// group_snapshot_id across multiple SPs.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub group_snapshot_id: ::prost::alloc::string::String,
    /// A list of snapshots belonging to this group.
    /// This field is REQUIRED.
    #[prost(message, repeated, tag = "2")]
    pub snapshots: ::prost::alloc::vec::Vec<Snapshot>,
    /// Timestamp of when the volume group snapshot was taken.
    /// This field is REQUIRED.
    #[prost(message, optional, tag = "3")]
    pub creation_time: ::core::option::Option<::prost_types::Timestamp>,
    /// Indicates if all individual snapshots in the group snapshot
    /// are ready to use as a `volume_content_source` in a
    /// `CreateVolumeRequest`. The default value is false.
    /// If any snapshot in the list of snapshots in this message have
    /// ready_to_use set to false, the SP MUST set this field to false.
    /// If all of the snapshots in the list of snapshots in this message
    /// have ready_to_use set to true, the SP SHOULD set this field to
    /// true.
    /// This field is REQUIRED.
    #[prost(bool, tag = "4")]
    pub ready_to_use: bool,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteVolumeGroupSnapshotRequest {
    /// The ID of the group snapshot to be deleted.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub group_snapshot_id: ::prost::alloc::string::String,
    /// A list of snapshot IDs that are part of this group snapshot.
    /// If SP does not need to rely on this field to delete the snapshots
    /// in the group, it SHOULD check this field and report an error
    /// if it has the ability to detect a mismatch.
    /// Some SPs require this list to delete the snapshots in the group.
    /// If SP needs to use this field to delete the snapshots in the
    /// group, it MUST report an error if it has the ability to detect
    /// a mismatch.
    /// This field is REQUIRED.
    #[prost(string, repeated, tag = "2")]
    pub snapshot_ids: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
    /// Secrets required by plugin to complete group snapshot deletion
    /// request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    /// The secrets provided in this field SHOULD be the same for
    /// all group snapshot operations on the same group snapshot.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeleteVolumeGroupSnapshotResponse {}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetVolumeGroupSnapshotRequest {
    /// The ID of the group snapshot to fetch current group snapshot
    /// information for.
    /// This field is REQUIRED.
    #[prost(string, tag = "1")]
    pub group_snapshot_id: ::prost::alloc::string::String,
    /// A list of snapshot IDs that are part of this group snapshot.
    /// If SP does not need to rely on this field to get the snapshots
    /// in the group, it SHOULD check this field and report an error
    /// if it has the ability to detect a mismatch.
    /// Some SPs require this list to get the snapshots in the group.
    /// If SP needs to use this field to get the snapshots in the
    /// group, it MUST report an error if it has the ability to detect
    /// a mismatch.
    /// This field is REQUIRED.
    #[prost(string, repeated, tag = "2")]
    pub snapshot_ids: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
    /// Secrets required by plugin to complete
    /// GetVolumeGroupSnapshot request.
    /// This field is OPTIONAL. Refer to the `Secrets Requirements`
    /// section on how to use this field.
    /// The secrets provided in this field SHOULD be the same for
    /// all group snapshot operations on the same group snapshot.
    #[prost(map = "string, string", tag = "3")]
    pub secrets: ::std::collections::HashMap<
        ::prost::alloc::string::String,
        ::prost::alloc::string::String,
    >,
}
#[allow(clippy::derive_partial_eq_without_eq)]
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct GetVolumeGroupSnapshotResponse {
    /// This field is REQUIRED
    #[prost(message, optional, tag = "1")]
    pub group_snapshot: ::core::option::Option<VolumeGroupSnapshot>,
}
/// Generated client implementations.
pub mod identity_client {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    use tonic::codegen::http::Uri;
    #[derive(Debug, Clone)]
    pub struct IdentityClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl<T> IdentityClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::BoxBody>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> IdentityClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
                Response = http::Response<
                    <T as tonic::client::GrpcService<tonic::body::BoxBody>>::ResponseBody,
                >,
            >,
            <T as tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
            >>::Error: Into<StdError> + Send + Sync,
        {
            IdentityClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn get_plugin_info(
            &mut self,
            request: impl tonic::IntoRequest<super::GetPluginInfoRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetPluginInfoResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Identity/GetPluginInfo",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Identity", "GetPluginInfo"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn get_plugin_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<super::GetPluginCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetPluginCapabilitiesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Identity/GetPluginCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Identity", "GetPluginCapabilities"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn probe(
            &mut self,
            request: impl tonic::IntoRequest<super::ProbeRequest>,
        ) -> std::result::Result<tonic::Response<super::ProbeResponse>, tonic::Status> {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/csi.v1.Identity/Probe");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new("csi.v1.Identity", "Probe"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated client implementations.
pub mod controller_client {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    use tonic::codegen::http::Uri;
    #[derive(Debug, Clone)]
    pub struct ControllerClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl<T> ControllerClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::BoxBody>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> ControllerClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
                Response = http::Response<
                    <T as tonic::client::GrpcService<tonic::body::BoxBody>>::ResponseBody,
                >,
            >,
            <T as tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
            >>::Error: Into<StdError> + Send + Sync,
        {
            ControllerClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn create_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/CreateVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "CreateVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn delete_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/DeleteVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "DeleteVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_publish_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerPublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerPublishVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerPublishVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ControllerPublishVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_unpublish_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerUnpublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerUnpublishVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerUnpublishVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new("csi.v1.Controller", "ControllerUnpublishVolume"),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn validate_volume_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<super::ValidateVolumeCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ValidateVolumeCapabilitiesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ValidateVolumeCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new("csi.v1.Controller", "ValidateVolumeCapabilities"),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn list_volumes(
            &mut self,
            request: impl tonic::IntoRequest<super::ListVolumesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListVolumesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ListVolumes",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ListVolumes"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn get_capacity(
            &mut self,
            request: impl tonic::IntoRequest<super::GetCapacityRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetCapacityResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/GetCapacity",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "GetCapacity"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_get_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerGetCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerGetCapabilitiesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerGetCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new("csi.v1.Controller", "ControllerGetCapabilities"),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn create_snapshot(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateSnapshotResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/CreateSnapshot",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "CreateSnapshot"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn delete_snapshot(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteSnapshotResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/DeleteSnapshot",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "DeleteSnapshot"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn list_snapshots(
            &mut self,
            request: impl tonic::IntoRequest<super::ListSnapshotsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListSnapshotsResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ListSnapshots",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ListSnapshots"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_expand_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerExpandVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerExpandVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerExpandVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ControllerExpandVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_get_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerGetVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerGetVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerGetVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ControllerGetVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn controller_modify_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::ControllerModifyVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerModifyVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Controller/ControllerModifyVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Controller", "ControllerModifyVolume"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated client implementations.
pub mod group_controller_client {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    use tonic::codegen::http::Uri;
    #[derive(Debug, Clone)]
    pub struct GroupControllerClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl<T> GroupControllerClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::BoxBody>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> GroupControllerClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
                Response = http::Response<
                    <T as tonic::client::GrpcService<tonic::body::BoxBody>>::ResponseBody,
                >,
            >,
            <T as tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
            >>::Error: Into<StdError> + Send + Sync,
        {
            GroupControllerClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn group_controller_get_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<
                super::GroupControllerGetCapabilitiesRequest,
            >,
        ) -> std::result::Result<
            tonic::Response<super::GroupControllerGetCapabilitiesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.GroupController/GroupControllerGetCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new(
                        "csi.v1.GroupController",
                        "GroupControllerGetCapabilities",
                    ),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn create_volume_group_snapshot(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateVolumeGroupSnapshotResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.GroupController/CreateVolumeGroupSnapshot",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new(
                        "csi.v1.GroupController",
                        "CreateVolumeGroupSnapshot",
                    ),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn delete_volume_group_snapshot(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteVolumeGroupSnapshotResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.GroupController/DeleteVolumeGroupSnapshot",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new(
                        "csi.v1.GroupController",
                        "DeleteVolumeGroupSnapshot",
                    ),
                );
            self.inner.unary(req, path, codec).await
        }
        pub async fn get_volume_group_snapshot(
            &mut self,
            request: impl tonic::IntoRequest<super::GetVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetVolumeGroupSnapshotResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.GroupController/GetVolumeGroupSnapshot",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(
                    GrpcMethod::new("csi.v1.GroupController", "GetVolumeGroupSnapshot"),
                );
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated client implementations.
pub mod node_client {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    use tonic::codegen::http::Uri;
    #[derive(Debug, Clone)]
    pub struct NodeClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl<T> NodeClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::BoxBody>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> NodeClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
                Response = http::Response<
                    <T as tonic::client::GrpcService<tonic::body::BoxBody>>::ResponseBody,
                >,
            >,
            <T as tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
            >>::Error: Into<StdError> + Send + Sync,
        {
            NodeClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn node_stage_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeStageVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeStageVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeStageVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeStageVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_unstage_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeUnstageVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeUnstageVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeUnstageVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeUnstageVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_publish_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::NodePublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodePublishVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodePublishVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodePublishVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_unpublish_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeUnpublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeUnpublishVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeUnpublishVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeUnpublishVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_get_volume_stats(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeGetVolumeStatsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetVolumeStatsResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeGetVolumeStats",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeGetVolumeStats"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_expand_volume(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeExpandVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeExpandVolumeResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeExpandVolume",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeExpandVolume"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_get_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeGetCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetCapabilitiesResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/csi.v1.Node/NodeGetCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("csi.v1.Node", "NodeGetCapabilities"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn node_get_info(
            &mut self,
            request: impl tonic::IntoRequest<super::NodeGetInfoRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetInfoResponse>,
            tonic::Status,
        > {
            self.inner
                .ready()
                .await
                .map_err(|e| {
                    tonic::Status::new(
                        tonic::Code::Unknown,
                        format!("Service was not ready: {}", e.into()),
                    )
                })?;
            let codec = tonic::codec::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/csi.v1.Node/NodeGetInfo");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new("csi.v1.Node", "NodeGetInfo"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod identity_server {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with IdentityServer.
    #[async_trait]
    pub trait Identity: Send + Sync + 'static {
        async fn get_plugin_info(
            &self,
            request: tonic::Request<super::GetPluginInfoRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetPluginInfoResponse>,
            tonic::Status,
        >;
        async fn get_plugin_capabilities(
            &self,
            request: tonic::Request<super::GetPluginCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetPluginCapabilitiesResponse>,
            tonic::Status,
        >;
        async fn probe(
            &self,
            request: tonic::Request<super::ProbeRequest>,
        ) -> std::result::Result<tonic::Response<super::ProbeResponse>, tonic::Status>;
    }
    #[derive(Debug)]
    pub struct IdentityServer<T: Identity> {
        inner: _Inner<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    struct _Inner<T>(Arc<T>);
    impl<T: Identity> IdentityServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            let inner = _Inner(inner);
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for IdentityServer<T>
    where
        T: Identity,
        B: Body + Send + 'static,
        B::Error: Into<StdError> + Send + 'static,
    {
        type Response = http::Response<tonic::body::BoxBody>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            let inner = self.inner.clone();
            match req.uri().path() {
                "/csi.v1.Identity/GetPluginInfo" => {
                    #[allow(non_camel_case_types)]
                    struct GetPluginInfoSvc<T: Identity>(pub Arc<T>);
                    impl<
                        T: Identity,
                    > tonic::server::UnaryService<super::GetPluginInfoRequest>
                    for GetPluginInfoSvc<T> {
                        type Response = super::GetPluginInfoResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetPluginInfoRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Identity>::get_plugin_info(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = GetPluginInfoSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Identity/GetPluginCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct GetPluginCapabilitiesSvc<T: Identity>(pub Arc<T>);
                    impl<
                        T: Identity,
                    > tonic::server::UnaryService<super::GetPluginCapabilitiesRequest>
                    for GetPluginCapabilitiesSvc<T> {
                        type Response = super::GetPluginCapabilitiesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetPluginCapabilitiesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Identity>::get_plugin_capabilities(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = GetPluginCapabilitiesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Identity/Probe" => {
                    #[allow(non_camel_case_types)]
                    struct ProbeSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::ProbeRequest>
                    for ProbeSvc<T> {
                        type Response = super::ProbeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ProbeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Identity>::probe(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ProbeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => {
                    Box::pin(async move {
                        Ok(
                            http::Response::builder()
                                .status(200)
                                .header("grpc-status", "12")
                                .header("content-type", "application/grpc")
                                .body(empty_body())
                                .unwrap(),
                        )
                    })
                }
            }
        }
    }
    impl<T: Identity> Clone for IdentityServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    impl<T: Identity> Clone for _Inner<T> {
        fn clone(&self) -> Self {
            Self(Arc::clone(&self.0))
        }
    }
    impl<T: std::fmt::Debug> std::fmt::Debug for _Inner<T> {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{:?}", self.0)
        }
    }
    impl<T: Identity> tonic::server::NamedService for IdentityServer<T> {
        const NAME: &'static str = "csi.v1.Identity";
    }
}
/// Generated server implementations.
pub mod controller_server {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with ControllerServer.
    #[async_trait]
    pub trait Controller: Send + Sync + 'static {
        async fn create_volume(
            &self,
            request: tonic::Request<super::CreateVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateVolumeResponse>,
            tonic::Status,
        >;
        async fn delete_volume(
            &self,
            request: tonic::Request<super::DeleteVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteVolumeResponse>,
            tonic::Status,
        >;
        async fn controller_publish_volume(
            &self,
            request: tonic::Request<super::ControllerPublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerPublishVolumeResponse>,
            tonic::Status,
        >;
        async fn controller_unpublish_volume(
            &self,
            request: tonic::Request<super::ControllerUnpublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerUnpublishVolumeResponse>,
            tonic::Status,
        >;
        async fn validate_volume_capabilities(
            &self,
            request: tonic::Request<super::ValidateVolumeCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ValidateVolumeCapabilitiesResponse>,
            tonic::Status,
        >;
        async fn list_volumes(
            &self,
            request: tonic::Request<super::ListVolumesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListVolumesResponse>,
            tonic::Status,
        >;
        async fn get_capacity(
            &self,
            request: tonic::Request<super::GetCapacityRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetCapacityResponse>,
            tonic::Status,
        >;
        async fn controller_get_capabilities(
            &self,
            request: tonic::Request<super::ControllerGetCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerGetCapabilitiesResponse>,
            tonic::Status,
        >;
        async fn create_snapshot(
            &self,
            request: tonic::Request<super::CreateSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateSnapshotResponse>,
            tonic::Status,
        >;
        async fn delete_snapshot(
            &self,
            request: tonic::Request<super::DeleteSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteSnapshotResponse>,
            tonic::Status,
        >;
        async fn list_snapshots(
            &self,
            request: tonic::Request<super::ListSnapshotsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListSnapshotsResponse>,
            tonic::Status,
        >;
        async fn controller_expand_volume(
            &self,
            request: tonic::Request<super::ControllerExpandVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerExpandVolumeResponse>,
            tonic::Status,
        >;
        async fn controller_get_volume(
            &self,
            request: tonic::Request<super::ControllerGetVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerGetVolumeResponse>,
            tonic::Status,
        >;
        async fn controller_modify_volume(
            &self,
            request: tonic::Request<super::ControllerModifyVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ControllerModifyVolumeResponse>,
            tonic::Status,
        >;
    }
    #[derive(Debug)]
    pub struct ControllerServer<T: Controller> {
        inner: _Inner<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    struct _Inner<T>(Arc<T>);
    impl<T: Controller> ControllerServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            let inner = _Inner(inner);
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for ControllerServer<T>
    where
        T: Controller,
        B: Body + Send + 'static,
        B::Error: Into<StdError> + Send + 'static,
    {
        type Response = http::Response<tonic::body::BoxBody>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            let inner = self.inner.clone();
            match req.uri().path() {
                "/csi.v1.Controller/CreateVolume" => {
                    #[allow(non_camel_case_types)]
                    struct CreateVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::CreateVolumeRequest>
                    for CreateVolumeSvc<T> {
                        type Response = super::CreateVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::create_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = CreateVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/DeleteVolume" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::DeleteVolumeRequest>
                    for DeleteVolumeSvc<T> {
                        type Response = super::DeleteVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::delete_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = DeleteVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerPublishVolume" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerPublishVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ControllerPublishVolumeRequest>
                    for ControllerPublishVolumeSvc<T> {
                        type Response = super::ControllerPublishVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ControllerPublishVolumeRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_publish_volume(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerPublishVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerUnpublishVolume" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerUnpublishVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<
                        super::ControllerUnpublishVolumeRequest,
                    > for ControllerUnpublishVolumeSvc<T> {
                        type Response = super::ControllerUnpublishVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ControllerUnpublishVolumeRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_unpublish_volume(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerUnpublishVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ValidateVolumeCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct ValidateVolumeCapabilitiesSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<
                        super::ValidateVolumeCapabilitiesRequest,
                    > for ValidateVolumeCapabilitiesSvc<T> {
                        type Response = super::ValidateVolumeCapabilitiesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ValidateVolumeCapabilitiesRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::validate_volume_capabilities(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ValidateVolumeCapabilitiesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ListVolumes" => {
                    #[allow(non_camel_case_types)]
                    struct ListVolumesSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ListVolumesRequest>
                    for ListVolumesSvc<T> {
                        type Response = super::ListVolumesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListVolumesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::list_volumes(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ListVolumesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/GetCapacity" => {
                    #[allow(non_camel_case_types)]
                    struct GetCapacitySvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::GetCapacityRequest>
                    for GetCapacitySvc<T> {
                        type Response = super::GetCapacityResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetCapacityRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::get_capacity(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = GetCapacitySvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerGetCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerGetCapabilitiesSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<
                        super::ControllerGetCapabilitiesRequest,
                    > for ControllerGetCapabilitiesSvc<T> {
                        type Response = super::ControllerGetCapabilitiesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ControllerGetCapabilitiesRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_get_capabilities(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerGetCapabilitiesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/CreateSnapshot" => {
                    #[allow(non_camel_case_types)]
                    struct CreateSnapshotSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::CreateSnapshotRequest>
                    for CreateSnapshotSvc<T> {
                        type Response = super::CreateSnapshotResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateSnapshotRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::create_snapshot(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = CreateSnapshotSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/DeleteSnapshot" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteSnapshotSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::DeleteSnapshotRequest>
                    for DeleteSnapshotSvc<T> {
                        type Response = super::DeleteSnapshotResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteSnapshotRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::delete_snapshot(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = DeleteSnapshotSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ListSnapshots" => {
                    #[allow(non_camel_case_types)]
                    struct ListSnapshotsSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ListSnapshotsRequest>
                    for ListSnapshotsSvc<T> {
                        type Response = super::ListSnapshotsResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListSnapshotsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::list_snapshots(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ListSnapshotsSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerExpandVolume" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerExpandVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ControllerExpandVolumeRequest>
                    for ControllerExpandVolumeSvc<T> {
                        type Response = super::ControllerExpandVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ControllerExpandVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_expand_volume(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerExpandVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerGetVolume" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerGetVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ControllerGetVolumeRequest>
                    for ControllerGetVolumeSvc<T> {
                        type Response = super::ControllerGetVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ControllerGetVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_get_volume(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerGetVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Controller/ControllerModifyVolume" => {
                    #[allow(non_camel_case_types)]
                    struct ControllerModifyVolumeSvc<T: Controller>(pub Arc<T>);
                    impl<
                        T: Controller,
                    > tonic::server::UnaryService<super::ControllerModifyVolumeRequest>
                    for ControllerModifyVolumeSvc<T> {
                        type Response = super::ControllerModifyVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ControllerModifyVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Controller>::controller_modify_volume(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = ControllerModifyVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => {
                    Box::pin(async move {
                        Ok(
                            http::Response::builder()
                                .status(200)
                                .header("grpc-status", "12")
                                .header("content-type", "application/grpc")
                                .body(empty_body())
                                .unwrap(),
                        )
                    })
                }
            }
        }
    }
    impl<T: Controller> Clone for ControllerServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    impl<T: Controller> Clone for _Inner<T> {
        fn clone(&self) -> Self {
            Self(Arc::clone(&self.0))
        }
    }
    impl<T: std::fmt::Debug> std::fmt::Debug for _Inner<T> {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{:?}", self.0)
        }
    }
    impl<T: Controller> tonic::server::NamedService for ControllerServer<T> {
        const NAME: &'static str = "csi.v1.Controller";
    }
}
/// Generated server implementations.
pub mod group_controller_server {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with GroupControllerServer.
    #[async_trait]
    pub trait GroupController: Send + Sync + 'static {
        async fn group_controller_get_capabilities(
            &self,
            request: tonic::Request<super::GroupControllerGetCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GroupControllerGetCapabilitiesResponse>,
            tonic::Status,
        >;
        async fn create_volume_group_snapshot(
            &self,
            request: tonic::Request<super::CreateVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::CreateVolumeGroupSnapshotResponse>,
            tonic::Status,
        >;
        async fn delete_volume_group_snapshot(
            &self,
            request: tonic::Request<super::DeleteVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::DeleteVolumeGroupSnapshotResponse>,
            tonic::Status,
        >;
        async fn get_volume_group_snapshot(
            &self,
            request: tonic::Request<super::GetVolumeGroupSnapshotRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetVolumeGroupSnapshotResponse>,
            tonic::Status,
        >;
    }
    #[derive(Debug)]
    pub struct GroupControllerServer<T: GroupController> {
        inner: _Inner<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    struct _Inner<T>(Arc<T>);
    impl<T: GroupController> GroupControllerServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            let inner = _Inner(inner);
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for GroupControllerServer<T>
    where
        T: GroupController,
        B: Body + Send + 'static,
        B::Error: Into<StdError> + Send + 'static,
    {
        type Response = http::Response<tonic::body::BoxBody>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            let inner = self.inner.clone();
            match req.uri().path() {
                "/csi.v1.GroupController/GroupControllerGetCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct GroupControllerGetCapabilitiesSvc<T: GroupController>(
                        pub Arc<T>,
                    );
                    impl<
                        T: GroupController,
                    > tonic::server::UnaryService<
                        super::GroupControllerGetCapabilitiesRequest,
                    > for GroupControllerGetCapabilitiesSvc<T> {
                        type Response = super::GroupControllerGetCapabilitiesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::GroupControllerGetCapabilitiesRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as GroupController>::group_controller_get_capabilities(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = GroupControllerGetCapabilitiesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.GroupController/CreateVolumeGroupSnapshot" => {
                    #[allow(non_camel_case_types)]
                    struct CreateVolumeGroupSnapshotSvc<T: GroupController>(pub Arc<T>);
                    impl<
                        T: GroupController,
                    > tonic::server::UnaryService<
                        super::CreateVolumeGroupSnapshotRequest,
                    > for CreateVolumeGroupSnapshotSvc<T> {
                        type Response = super::CreateVolumeGroupSnapshotResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::CreateVolumeGroupSnapshotRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as GroupController>::create_volume_group_snapshot(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = CreateVolumeGroupSnapshotSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.GroupController/DeleteVolumeGroupSnapshot" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteVolumeGroupSnapshotSvc<T: GroupController>(pub Arc<T>);
                    impl<
                        T: GroupController,
                    > tonic::server::UnaryService<
                        super::DeleteVolumeGroupSnapshotRequest,
                    > for DeleteVolumeGroupSnapshotSvc<T> {
                        type Response = super::DeleteVolumeGroupSnapshotResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::DeleteVolumeGroupSnapshotRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as GroupController>::delete_volume_group_snapshot(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = DeleteVolumeGroupSnapshotSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.GroupController/GetVolumeGroupSnapshot" => {
                    #[allow(non_camel_case_types)]
                    struct GetVolumeGroupSnapshotSvc<T: GroupController>(pub Arc<T>);
                    impl<
                        T: GroupController,
                    > tonic::server::UnaryService<super::GetVolumeGroupSnapshotRequest>
                    for GetVolumeGroupSnapshotSvc<T> {
                        type Response = super::GetVolumeGroupSnapshotResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetVolumeGroupSnapshotRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as GroupController>::get_volume_group_snapshot(
                                        &inner,
                                        request,
                                    )
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = GetVolumeGroupSnapshotSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => {
                    Box::pin(async move {
                        Ok(
                            http::Response::builder()
                                .status(200)
                                .header("grpc-status", "12")
                                .header("content-type", "application/grpc")
                                .body(empty_body())
                                .unwrap(),
                        )
                    })
                }
            }
        }
    }
    impl<T: GroupController> Clone for GroupControllerServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    impl<T: GroupController> Clone for _Inner<T> {
        fn clone(&self) -> Self {
            Self(Arc::clone(&self.0))
        }
    }
    impl<T: std::fmt::Debug> std::fmt::Debug for _Inner<T> {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{:?}", self.0)
        }
    }
    impl<T: GroupController> tonic::server::NamedService for GroupControllerServer<T> {
        const NAME: &'static str = "csi.v1.GroupController";
    }
}
/// Generated server implementations.
pub mod node_server {
    #![allow(unused_variables, dead_code, missing_docs, clippy::let_unit_value)]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with NodeServer.
    #[async_trait]
    pub trait Node: Send + Sync + 'static {
        async fn node_stage_volume(
            &self,
            request: tonic::Request<super::NodeStageVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeStageVolumeResponse>,
            tonic::Status,
        >;
        async fn node_unstage_volume(
            &self,
            request: tonic::Request<super::NodeUnstageVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeUnstageVolumeResponse>,
            tonic::Status,
        >;
        async fn node_publish_volume(
            &self,
            request: tonic::Request<super::NodePublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodePublishVolumeResponse>,
            tonic::Status,
        >;
        async fn node_unpublish_volume(
            &self,
            request: tonic::Request<super::NodeUnpublishVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeUnpublishVolumeResponse>,
            tonic::Status,
        >;
        async fn node_get_volume_stats(
            &self,
            request: tonic::Request<super::NodeGetVolumeStatsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetVolumeStatsResponse>,
            tonic::Status,
        >;
        async fn node_expand_volume(
            &self,
            request: tonic::Request<super::NodeExpandVolumeRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeExpandVolumeResponse>,
            tonic::Status,
        >;
        async fn node_get_capabilities(
            &self,
            request: tonic::Request<super::NodeGetCapabilitiesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetCapabilitiesResponse>,
            tonic::Status,
        >;
        async fn node_get_info(
            &self,
            request: tonic::Request<super::NodeGetInfoRequest>,
        ) -> std::result::Result<
            tonic::Response<super::NodeGetInfoResponse>,
            tonic::Status,
        >;
    }
    #[derive(Debug)]
    pub struct NodeServer<T: Node> {
        inner: _Inner<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    struct _Inner<T>(Arc<T>);
    impl<T: Node> NodeServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            let inner = _Inner(inner);
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for NodeServer<T>
    where
        T: Node,
        B: Body + Send + 'static,
        B::Error: Into<StdError> + Send + 'static,
    {
        type Response = http::Response<tonic::body::BoxBody>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            let inner = self.inner.clone();
            match req.uri().path() {
                "/csi.v1.Node/NodeStageVolume" => {
                    #[allow(non_camel_case_types)]
                    struct NodeStageVolumeSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeStageVolumeRequest>
                    for NodeStageVolumeSvc<T> {
                        type Response = super::NodeStageVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeStageVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_stage_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeStageVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeUnstageVolume" => {
                    #[allow(non_camel_case_types)]
                    struct NodeUnstageVolumeSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeUnstageVolumeRequest>
                    for NodeUnstageVolumeSvc<T> {
                        type Response = super::NodeUnstageVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeUnstageVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_unstage_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeUnstageVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodePublishVolume" => {
                    #[allow(non_camel_case_types)]
                    struct NodePublishVolumeSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodePublishVolumeRequest>
                    for NodePublishVolumeSvc<T> {
                        type Response = super::NodePublishVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodePublishVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_publish_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodePublishVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeUnpublishVolume" => {
                    #[allow(non_camel_case_types)]
                    struct NodeUnpublishVolumeSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeUnpublishVolumeRequest>
                    for NodeUnpublishVolumeSvc<T> {
                        type Response = super::NodeUnpublishVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeUnpublishVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_unpublish_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeUnpublishVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeGetVolumeStats" => {
                    #[allow(non_camel_case_types)]
                    struct NodeGetVolumeStatsSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeGetVolumeStatsRequest>
                    for NodeGetVolumeStatsSvc<T> {
                        type Response = super::NodeGetVolumeStatsResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeGetVolumeStatsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_get_volume_stats(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeGetVolumeStatsSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeExpandVolume" => {
                    #[allow(non_camel_case_types)]
                    struct NodeExpandVolumeSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeExpandVolumeRequest>
                    for NodeExpandVolumeSvc<T> {
                        type Response = super::NodeExpandVolumeResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeExpandVolumeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_expand_volume(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeExpandVolumeSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeGetCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct NodeGetCapabilitiesSvc<T: Node>(pub Arc<T>);
                    impl<
                        T: Node,
                    > tonic::server::UnaryService<super::NodeGetCapabilitiesRequest>
                    for NodeGetCapabilitiesSvc<T> {
                        type Response = super::NodeGetCapabilitiesResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeGetCapabilitiesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_get_capabilities(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeGetCapabilitiesSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/csi.v1.Node/NodeGetInfo" => {
                    #[allow(non_camel_case_types)]
                    struct NodeGetInfoSvc<T: Node>(pub Arc<T>);
                    impl<T: Node> tonic::server::UnaryService<super::NodeGetInfoRequest>
                    for NodeGetInfoSvc<T> {
                        type Response = super::NodeGetInfoResponse;
                        type Future = BoxFuture<
                            tonic::Response<Self::Response>,
                            tonic::Status,
                        >;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::NodeGetInfoRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Node>::node_get_info(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let inner = inner.0;
                        let method = NodeGetInfoSvc(inner);
                        let codec = tonic::codec::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => {
                    Box::pin(async move {
                        Ok(
                            http::Response::builder()
                                .status(200)
                                .header("grpc-status", "12")
                                .header("content-type", "application/grpc")
                                .body(empty_body())
                                .unwrap(),
                        )
                    })
                }
            }
        }
    }
    impl<T: Node> Clone for NodeServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    impl<T: Node> Clone for _Inner<T> {
        fn clone(&self) -> Self {
            Self(Arc::clone(&self.0))
        }
    }
    impl<T: std::fmt::Debug> std::fmt::Debug for _Inner<T> {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{:?}", self.0)
        }
    }
    impl<T: Node> tonic::server::NamedService for NodeServer<T> {
        const NAME: &'static str = "csi.v1.Node";
    }
}
