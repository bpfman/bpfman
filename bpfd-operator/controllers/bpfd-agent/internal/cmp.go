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

	gobpfd "github.com/bpfd-dev/bpfd/clients/gobpfd/v1"
)

// Look at using https://pkg.go.dev/google.golang.org/protobuf/testing/protocmp to simplify.
// Is state equal, ignoring UUID and GRPC type fields.
func DoesProgExist(actual *gobpfd.ListResponse_ListResult, expected *gobpfd.LoadRequest) (bool, []string) {
	var reasons []string

	// Check equality of all common fields
	actualId := actual.GetId()
	expectedId := expected.Common.GetId()
	if actualId != expectedId {
		reasons = append(reasons, fmt.Sprintf("Expected ID to be %s but found %s",
			expectedId, actualId))
	}

	actualName := actual.GetName()
	expectedSectionName := expected.Common.GetSectionName()
	if actualName != expectedSectionName {
		reasons = append(reasons, fmt.Sprintf("Expected Name to be %s but found %s",
			expectedSectionName, actualName))
	}

	actualProgramType := actual.GetProgramType()
	expectedProgramType := expected.Common.GetProgramType()
	if actualProgramType != expectedProgramType {
		reasons = append(reasons, fmt.Sprintf("Expected ProgramType to be %d but found %d",
			expectedProgramType, actualProgramType))
	}

	// Check equality of all bytecode location fields
	actualImage := actual.GetImage()
	expectedImage := expected.Common.GetImage()
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

	actualFile := actual.GetFile()
	expectedFile := expected.Common.GetFile()
	if actualFile != expectedFile {
		reasons = append(reasons, fmt.Sprintf("Expected File to be %s but found %s",
			expectedFile, actualFile))
	}

	// Check equality of Map Owner
	actualMapOwnerUuid := actual.GetMapOwnerUuid()
	expectedMapOwnerUuid := expected.Common.GetMapOwnerUuid()
	if actualMapOwnerUuid != expectedMapOwnerUuid {
		reasons = append(reasons, fmt.Sprintf("Expected File to be %s but found %s",
			expectedMapOwnerUuid, actualMapOwnerUuid))
	}

	// Check equality of program specific fields
	actualXdp := actual.GetXdpAttachInfo()
	expectedXdp := expected.GetXdpAttachInfo()
	if actualXdp != nil && expectedXdp != nil {
		if actualXdp.Priority != expectedXdp.Priority ||
			actualXdp.Iface != expectedXdp.Iface ||
			!reflect.DeepEqual(actualXdp.ProceedOn, expectedXdp.ProceedOn) {
			reasons = append(reasons, fmt.Sprintf("Expected XDP to be %v but found %v",
				expectedXdp, actualXdp))
		}
	}

	actualTc := actual.GetTcAttachInfo()
	expectedTc := expected.GetTcAttachInfo()
	if actualTc != nil && expectedTc != nil {
		if actualTc.Priority != expectedTc.Priority ||
			actualTc.Iface != expectedTc.Iface ||
			!reflect.DeepEqual(actualTc.ProceedOn, expectedTc.ProceedOn) {
			reasons = append(reasons, fmt.Sprintf("Expected TC to be %v but found %v",
				expectedTc, actualTc))
		}
	}

	actualTracepoint := actual.GetTracepointAttachInfo()
	expectedTracepoint := expected.GetTracepointAttachInfo()
	if actualTracepoint != nil && expectedTracepoint != nil {
		if actualTracepoint.Tracepoint != expectedTracepoint.Tracepoint {
			reasons = append(reasons, fmt.Sprintf("Expected Tracepoint to be %v but found %v",
				expectedTracepoint, actualTracepoint))
		}
	}

	actualKprobe := actual.GetKprobeAttachInfo()
	expectedKprobe := expected.GetKprobeAttachInfo()
	if actualKprobe != nil && expectedKprobe != nil {
		if actualKprobe.FnName != expectedKprobe.FnName ||
			actualKprobe.Offset != expectedKprobe.Offset ||
			actualKprobe.Retprobe != expectedKprobe.Retprobe ||
			actualKprobe.Namespace != expectedKprobe.Namespace {
			reasons = append(reasons, fmt.Sprintf("Expected Kprobe to be %v but found %v",
				expectedKprobe, actualKprobe))
		}
	}

	if len(reasons) == 0 {
		return true, reasons
	} else {
		return false, reasons
	}
}
