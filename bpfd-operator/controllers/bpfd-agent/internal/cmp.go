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
	"reflect"

	gobpfd "github.com/redhat-et/bpfd/clients/gobpfd/v1"
)

// Look at using https://pkg.go.dev/google.golang.org/protobuf/testing/protocmp to simplify.
// Is state equal, ignoring UUID and GRPC type fields.
func DoesProgExist(actual *gobpfd.ListResponse_ListResult, expected *gobpfd.LoadRequest) bool {
	// Check equality of all common fields
	if actual.Id != *expected.Common.Id &&
		*actual.SectionName != expected.Common.SectionName &&
		actual.ProgramType != expected.Common.ProgramType {
		return false
	}

	// Check equality of all bytecode location fields
	actualImage := actual.GetImage()
	expectedImage := expected.Common.GetImage()
	if actualImage != nil && expectedImage != nil {
		if actualImage.Url != expectedImage.Url &&
			actualImage.ImagePullPolicy != expectedImage.ImagePullPolicy {
			return false
		}

	}

	actualFile := actual.GetFile()
	expectedFile := expected.Common.GetFile()
	if actualFile != expectedFile {
		return false
	}

	// Check equality of program specific fields
	actualXdp := actual.GetXdpAttachInfo()
	expectedXdp := expected.GetXdpAttachInfo()
	if actualXdp != nil && expectedXdp != nil {
		if actualXdp.Priority != expectedXdp.Priority &&
			actualXdp.Iface != expectedXdp.Iface &&
			!reflect.DeepEqual(actualXdp.ProceedOn, expectedXdp.ProceedOn) {
			return false
		}
	}

	actualTc := actual.GetTcAttachInfo()
	expectedTc := expected.GetTcAttachInfo()
	if actualTc != nil && expectedTc != nil {
		if actualTc.Priority != expectedTc.Priority &&
			actualTc.Iface != expectedTc.Iface &&
			!reflect.DeepEqual(actualTc.ProceedOn, expectedTc.ProceedOn) {
			return false
		}
	}

	actualTracepoint := actual.GetTracepointAttachInfo()
	expectedTracepoint := expected.GetTracepointAttachInfo()
	if actualTracepoint != nil && expectedTracepoint != nil {
		if actualTracepoint.Tracepoint != expectedTracepoint.Tracepoint {
			return false
		}
	}

	return true
}
