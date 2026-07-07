package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
)

// GetPluginInfo returns metadata about the CSI plugin.
func (d *Driver) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	d.logger.Debug("GetPluginInfo", "method", "Identity.GetPluginInfo")

	resp := &csi.GetPluginInfoResponse{
		Name:          d.name,
		VendorVersion: d.version,
	}

	d.logger.Info("GetPluginInfo response", "method", "Identity.GetPluginInfo", "name", resp.Name, "version", resp.VendorVersion)

	return resp, nil
}

// GetPluginCapabilities returns the capabilities of this CSI plugin.
func (d *Driver) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	d.logger.Debug("GetPluginCapabilities", "method", "Identity.GetPluginCapabilities")

	resp := &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{},
	}

	d.logger.Info("GetPluginCapabilities response", "method", "Identity.GetPluginCapabilities", "capabilities", len(resp.Capabilities))

	return resp, nil
}

// Probe checks if the plugin is healthy and ready to serve requests.
func (d *Driver) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	d.logger.Debug("Probe", "method", "Identity.Probe")

	resp := &csi.ProbeResponse{
		Ready: &wrappers.BoolValue{Value: true},
	}

	d.logger.Info("Probe response", "method", "Identity.Probe", "ready", true)

	return resp, nil
}
