/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package internal

import (
	"fmt"
	"reflect"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
)

// Look at using https://pkg.go.dev/google.golang.org/protobuf/testing/protocmp to simplify.
// Is state equal, ignoring UUID and GRPC type fields.
func DoesProgExist(actual *gobpfman.ListResponse_ListResult, expected *gobpfman.LoadRequest) (bool, []string) {
	var reasons []string

	actualInfo := actual.GetInfo()
	if actualInfo == nil {
		reasons = append(reasons, "Missing response data")
		return true, reasons
	}

	actualKernelInfo := actual.GetKernelInfo()
	if actualKernelInfo == nil {
		reasons = append(reasons, "Missing kernel response data")
		return true, reasons
	}

	// Check equality of all common fields
	actualMeta := actualInfo.GetMetadata()
	expectedMeta := expected.GetMetadata()
	if !reflect.DeepEqual(actualMeta, expectedMeta) {
		reasons = append(reasons, fmt.Sprintf("Expected ID to be %v but found %v",
			actualMeta, expectedMeta))
	}

	actualName := actualInfo.GetName()
	expectedBpfFunctionName := expected.GetName()
	if actualName != expectedBpfFunctionName {
		reasons = append(reasons, fmt.Sprintf("Expected Name to be %s but found %s",
			expectedBpfFunctionName, actualName))
	}

	actualProgramType := actualKernelInfo.GetProgramType()
	expectedProgramType := expected.GetProgramType()
	if actualProgramType != expectedProgramType {
		reasons = append(reasons, fmt.Sprintf("Expected ProgramType to be %d but found %d",
			expectedProgramType, actualProgramType))
	}

	// Check equality of all bytecode location fields
	actualBytecode := actualInfo.GetBytecode()
	expectedBytecode := expected.GetBytecode()
	if actualBytecode != nil && expectedBytecode != nil {
		actualImage := actualBytecode.GetImage()
		expectedImage := expectedBytecode.GetImage()
		if actualImage != nil && expectedImage != nil {
			if actualImage.Url != expectedImage.Url {
				reasons = append(reasons, fmt.Sprintf("Expected Image URL to be %s but found %s",
					expectedImage.Url, actualImage.Url))
			}
			if actualImage.ImagePullPolicy != expectedImage.ImagePullPolicy {
				reasons = append(reasons, fmt.Sprintf("Expected ImagePullPolicy to be %d but found %d",
					expectedImage.ImagePullPolicy, actualImage.ImagePullPolicy))
			}
		}

		actualFile := actualBytecode.GetFile()
		expectedFile := expectedBytecode.GetFile()
		if actualFile != expectedFile {
			reasons = append(reasons, fmt.Sprintf("Expected File to be %s but found %s",
				expectedFile, actualFile))
		}
	}

	// Check equality of Map Owner
	actualMapOwnerId := actualInfo.GetMapOwnerId()
	expectedMapOwnerId := expected.GetMapOwnerId()
	if actualMapOwnerId != expectedMapOwnerId {
		reasons = append(reasons, fmt.Sprintf("Expected File to be %d but found %d",
			expectedMapOwnerId, actualMapOwnerId))
	}

	// Check equality of program specific fields
	actualAttach := actualInfo.GetAttach()
	expectedAttach := expected.GetAttach()
	if actualAttach != nil && expectedAttach != nil {
		actualXdp := actualAttach.GetXdpAttachInfo()
		expectedXdp := expectedAttach.GetXdpAttachInfo()
		if actualXdp != nil && expectedXdp != nil {
			if actualXdp.Priority != expectedXdp.Priority ||
				actualXdp.Iface != expectedXdp.Iface ||
				!reflect.DeepEqual(actualXdp.ProceedOn, expectedXdp.ProceedOn) {
				reasons = append(reasons, fmt.Sprintf("Expected XDP to be %v but found %v",
					expectedXdp, actualXdp))
			}
		}

		actualTc := actualAttach.GetTcAttachInfo()
		expectedTc := expectedAttach.GetTcAttachInfo()
		if actualTc != nil && expectedTc != nil {
			if actualTc.Priority != expectedTc.Priority ||
				actualTc.Iface != expectedTc.Iface ||
				!reflect.DeepEqual(actualTc.ProceedOn, expectedTc.ProceedOn) {
				reasons = append(reasons, fmt.Sprintf("Expected TC to be %v but found %v",
					expectedTc, actualTc))
			}
		}

		actualTracepoint := actualAttach.GetTracepointAttachInfo()
		expectedTracepoint := expectedAttach.GetTracepointAttachInfo()
		if actualTracepoint != nil && expectedTracepoint != nil {
			if actualTracepoint.Tracepoint != expectedTracepoint.Tracepoint {
				reasons = append(reasons, fmt.Sprintf("Expected Tracepoint to be %v but found %v",
					expectedTracepoint, actualTracepoint))
			}
		}

		actualKprobe := actualAttach.GetKprobeAttachInfo()
		expectedKprobe := expectedAttach.GetKprobeAttachInfo()
		if actualKprobe != nil && expectedKprobe != nil {
			if actualKprobe.FnName != expectedKprobe.FnName ||
				actualKprobe.Offset != expectedKprobe.Offset ||
				actualKprobe.Retprobe != expectedKprobe.Retprobe ||
				!reflect.DeepEqual(actualKprobe.ContainerPid, expectedKprobe.ContainerPid) {
				reasons = append(reasons, fmt.Sprintf("Expected Kprobe to be %v but found %v",
					expectedKprobe, actualKprobe))
			}
		}

		actualUprobe := actualAttach.GetUprobeAttachInfo()
		expectedUprobe := expectedAttach.GetUprobeAttachInfo()
		if actualUprobe != nil && expectedUprobe != nil {
			if !reflect.DeepEqual(actualUprobe.FnName, expectedUprobe.FnName) ||
				actualUprobe.Offset != expectedUprobe.Offset ||
				actualUprobe.Target != expectedUprobe.Target ||
				actualUprobe.Retprobe != expectedUprobe.Retprobe ||
				actualUprobe.Pid != expectedUprobe.Pid ||
				!reflect.DeepEqual(actualUprobe.ContainerPid, expectedUprobe.ContainerPid) {
				reasons = append(reasons, fmt.Sprintf("Expected Uprobe to be %v but found %v",
					expectedUprobe, actualUprobe))
			}
		}
	}

	if len(reasons) == 0 {
		return true, reasons
	} else {
		return false, reasons
	}
}
