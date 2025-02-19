// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use shadow_rs::{BuildPattern, ShadowBuilder};

fn main() {
    ShadowBuilder::builder()
        .build_pattern(BuildPattern::RealTime)
        .build()
        .unwrap();
}
