// Package driver implements a Kubernetes Container Storage Interface (CSI)
// driver that exposes BPF maps to pods.
//
// # Overview
//
// The CSI driver enables Kubernetes workloads to access BPF maps created by
// bpfman-managed programs. When a pod requests a bpfman CSI volume, the driver
// locates the program by metadata, re-pins its maps to a per-pod bpffs, and
// bind-mounts that filesystem into the container.
//
// This allows user-space applications running in pods to read from or write to
// BPF maps without requiring elevated privileges or direct access to the host's
// bpffs.
//
// # Architecture
//
// The driver implements the CSI Node service, handling volume publish and
// unpublish operations. It does not implement the Controller service as there
// is no external storage to provision.
//
//	kubelet
//	   |
//	   v (gRPC over unix socket)
//	+--------------------------+
//	|     CSI Driver           |
//	|  +--------------------+  |
//	|  | Identity Service   |  |  GetPluginInfo, Probe
//	|  +--------------------+  |
//	|  +--------------------+  |
//	|  | Node Service       |  |  NodePublishVolume, NodeUnpublishVolume
//	|  +--------------------+  |
//	+--------------------------+
//	           |
//	           v
//	+--------------------------+
//	| bpfman (ProgramFinder)   |  Lookup program by metadata
//	+--------------------------+
//	           |
//	           v
//	+--------------------------+
//	| Kernel (RepinMap)        |  Re-pin maps to per-pod bpffs
//	+--------------------------+
//
// # Volume Lifecycle
//
// When a pod with a bpfman CSI volume starts:
//
//  1. kubelet calls NodePublishVolume with volume attributes
//  2. Driver looks up the program by bpfman.io/ProgramName metadata
//  3. Driver creates a per-pod bpffs at /run/bpfman/csi/fs/<volume-id>
//  4. Driver re-pins requested maps from the program's map directory
//  5. Driver bind-mounts the per-pod bpffs to the container's target path
//
// When the pod terminates:
//
//  1. kubelet calls NodeUnpublishVolume
//  2. Driver unmounts the bind-mount from the container
//  3. Driver unmounts and removes the per-pod bpffs
//
// # Volume Attributes
//
// Pods specify which program and maps to access via CSI volume attributes:
//
//	volumes:
//	  - name: bpf-maps
//	    csi:
//	      driver: csi.bpfman.io
//	      volumeAttributes:
//	        csi.bpfman.io/program: "my-program"    # matches bpfman.io/ProgramName
//	        csi.bpfman.io/maps: "stats,config"     # comma-separated map names
//
// The program attribute is matched against the bpfman.io/ProgramName metadata
// set when loading the program. The maps attribute lists which of the program's
// maps should be exposed in the volume.
//
// # Unprivileged Access
//
// By default, BPF maps are only accessible to root. The driver supports
// Kubernetes fsGroup to enable unprivileged container access:
//
//	securityContext:
//	  fsGroup: 1000
//
// When fsGroup is set, the driver:
//   - Changes group ownership of the per-pod bpffs directory
//   - Changes group ownership of each re-pinned map
//   - Sets permissions to 0660 (owner and group read/write)
//
// This allows containers running as non-root with the matching GID to access
// the maps.
//
// # Error Handling
//
// The driver returns appropriate gRPC status codes:
//
//   - NotFound: Program not yet loaded (operator may be starting up)
//   - FailedPrecondition: bpfman not configured, or the target path is
//     already mounted by another filesystem
//   - InvalidArgument: Missing required volume attributes
//   - Internal: Filesystem or kernel operation failed
//
// The NotFound case is expected during initial pod scheduling, as the bpfman
// operator may not have loaded the program yet. kubelet will retry until the
// program becomes available.
//
// # Example Usage
//
// Start the CSI driver:
//
//	driver := csi.New(
//	    "csi.bpfman.io",           // driver name
//	    "1.0.0",                    // version
//	    os.Getenv("NODE_NAME"),     // node ID
//	    "unix:///csi/csi.sock",     // endpoint
//	    logger,
//	    csi.WithProgramFinder(manager),
//	    csi.WithKernel(kernelOps),
//	)
//	if err := driver.Run(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Compatibility
//
// The volume attribute keys (csi.bpfman.io/program, csi.bpfman.io/maps) and
// metadata key (bpfman.io/ProgramName) match the upstream Rust bpfman
// implementation, ensuring compatibility with existing bpfman Kubernetes
// deployments.
package driver
