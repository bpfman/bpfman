// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

//! Read-only dispatcher snapshots for the `bpfman dispatcher list|get`
//! CLI, assembled from the sled store.
//!
//! The types are serde-serialisable so the output can be diffed against
//! the Go implementation of bpfman. Field names mirror the Go JSON
//! shape (`key`, `revision`, `runtime`, `members`, ...).
//!
//! Some fields Go reports are kernel-assigned identifiers that this
//! implementation does not persist -- they live only on the loaded BPF
//! objects, not in the store. Those are the dispatcher's own program id
//! and link id, and each member's kernel link id. They are reported as
//! null rather than loaded from the pins; the kernel hands them out at
//! load time, so they differ between runs and carry no parity meaning.

use std::collections::BTreeMap;

use serde::Serialize;
use sled::Db;

use crate::{
    errors::BpfmanError,
    get_dispatcher, get_multi_attach_links,
    multiprog::{
        Dispatcher, DispatcherId, DispatcherInfo, TC_DISPATCHER_PREFIX, XDP_DISPATCHER_PREFIX,
    },
    types::{BpfProgType, Direction, Link},
    utils::bytes_to_string,
};

/// Attach-point key identifying a dispatcher.
#[derive(Debug, Clone, Serialize)]
pub struct DispatcherKey {
    #[serde(rename = "type")]
    pub dispatcher_type: String,
    pub nsid: u64,
    pub ifindex: u32,
}

/// Kernel-assigned runtime identifiers for the dispatcher.
#[derive(Debug, Clone, Serialize)]
pub struct DispatcherRuntime {
    /// Kernel program id of the dispatcher. Not persisted; always null.
    pub program_id: Option<u32>,
    /// Kernel link id of the dispatcher. Not persisted; always null.
    pub kernel_link_id: Option<u32>,
    /// TC filter priority. Set for TC dispatchers, absent for XDP.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub filter_priority: Option<u16>,
    /// TC filter handle. Set for TC dispatchers, absent for XDP.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub filter_handle: Option<u32>,
    pub netns_path: String,
}

/// One extension program attached to a dispatcher.
#[derive(Debug, Clone, Serialize)]
pub struct DispatcherMember {
    /// Kernel program id of the extension program.
    pub program_id: u32,
    pub program_name: String,
    /// bpfman link id.
    pub link_id: u32,
    /// Kernel link id of the extension. Not persisted; always null.
    pub kernel_link_id: Option<u32>,
    /// Slot the dispatcher runs the program in (0-based, ascending is
    /// execution order).
    pub position: Option<usize>,
    pub priority: i32,
    pub proceed_on: u32,
}

/// A full dispatcher view including its members.
#[derive(Debug, Clone, Serialize)]
pub struct DispatcherSnapshot {
    pub key: DispatcherKey,
    pub revision: u32,
    pub runtime: DispatcherRuntime,
    pub members: Vec<DispatcherMember>,
}

/// A lightweight dispatcher view carrying the member count rather than
/// the full member list.
#[derive(Debug, Clone, Serialize)]
pub struct DispatcherSummary {
    pub key: DispatcherKey,
    pub revision: u32,
    pub runtime: DispatcherRuntime,
    pub member_count: usize,
}

/// Return every dispatcher present in the store, one summary each.
pub fn list_dispatcher_snapshots(root_db: &Db) -> Result<Vec<DispatcherSummary>, BpfmanError> {
    // A dispatcher may briefly have two revision trees during an atomic
    // swap; keep the highest-revision one per attach point.
    let mut by_key: BTreeMap<(String, u64, u32), DispatcherSummary> = BTreeMap::new();

    for name in root_db.tree_names() {
        let tree_name = bytes_to_string(&name);
        if !tree_name.contains(XDP_DISPATCHER_PREFIX) && !tree_name.contains(TC_DISPATCHER_PREFIX) {
            continue;
        }

        let tree = root_db.open_tree(&name).map_err(|e| {
            BpfmanError::DatabaseError("unable to open dispatcher tree".to_string(), e.to_string())
        })?;
        let dispatcher = Dispatcher::new_from_db(tree);

        let snapshot = snapshot_from_dispatcher(root_db, &dispatcher)?;
        let dedup = (
            snapshot.key.dispatcher_type.clone(),
            snapshot.key.nsid,
            snapshot.key.ifindex,
        );
        let summary = DispatcherSummary {
            key: snapshot.key,
            revision: snapshot.revision,
            runtime: snapshot.runtime,
            member_count: snapshot.members.len(),
        };

        match by_key.get(&dedup) {
            Some(existing) if existing.revision >= summary.revision => {}
            _ => {
                by_key.insert(dedup, summary);
            }
        }
    }

    Ok(by_key.into_values().collect())
}

/// Return a single dispatcher by attach-point key, or None if no such
/// dispatcher exists.
pub fn get_dispatcher_snapshot(
    root_db: &Db,
    dispatcher_type: &str,
    nsid: u64,
    ifindex: u32,
) -> Result<Option<DispatcherSnapshot>, BpfmanError> {
    let id = dispatcher_id_from_type(dispatcher_type, nsid, ifindex)?;

    match get_dispatcher(&id, root_db)? {
        Some(dispatcher) => Ok(Some(snapshot_from_dispatcher(root_db, &dispatcher)?)),
        None => Ok(None),
    }
}

fn dispatcher_id_from_type(
    dispatcher_type: &str,
    nsid: u64,
    ifindex: u32,
) -> Result<DispatcherId, BpfmanError> {
    match dispatcher_type {
        "xdp" => Ok(DispatcherId::Xdp(DispatcherInfo(nsid, ifindex, None))),
        "tc-ingress" => Ok(DispatcherId::Tc(DispatcherInfo(
            nsid,
            ifindex,
            Some(Direction::Ingress),
        ))),
        "tc-egress" => Ok(DispatcherId::Tc(DispatcherInfo(
            nsid,
            ifindex,
            Some(Direction::Egress),
        ))),
        other => Err(BpfmanError::Error(format!(
            "unknown dispatcher type {other:?}; expected xdp, tc-ingress or tc-egress"
        ))),
    }
}

fn snapshot_from_dispatcher(
    root_db: &Db,
    dispatcher: &Dispatcher,
) -> Result<DispatcherSnapshot, BpfmanError> {
    let (key, revision, runtime, program_type, ifindex, direction, nsid) = match dispatcher {
        Dispatcher::Xdp(d) => {
            let nsid = d.get_nsid()?;
            let ifindex = d.get_ifindex()?;
            let key = DispatcherKey {
                dispatcher_type: "xdp".to_string(),
                nsid,
                ifindex,
            };
            let runtime = DispatcherRuntime {
                program_id: None,
                kernel_link_id: None,
                filter_priority: None,
                filter_handle: None,
                netns_path: String::new(),
            };
            (
                key,
                d.get_revision()?,
                runtime,
                BpfProgType::Xdp,
                ifindex,
                None,
                nsid,
            )
        }
        Dispatcher::Tc(d) => {
            let nsid = d.get_nsid()?;
            let ifindex = d.get_ifindex()?;
            let direction = d.get_direction()?;
            let dispatcher_type = match direction {
                Direction::Ingress => "tc-ingress",
                Direction::Egress => "tc-egress",
            };
            let key = DispatcherKey {
                dispatcher_type: dispatcher_type.to_string(),
                nsid,
                ifindex,
            };
            let netns_path = d
                .get_netns()?
                .map(|p| p.to_string_lossy().into_owned())
                .unwrap_or_default();
            let runtime = DispatcherRuntime {
                program_id: None,
                kernel_link_id: None,
                filter_priority: Some(d.get_priority()?),
                filter_handle: d.get_handle()?,
                netns_path,
            };
            (
                key,
                d.get_revision()?,
                runtime,
                BpfProgType::Tc,
                ifindex,
                Some(direction),
                nsid,
            )
        }
    };

    let links = get_multi_attach_links(root_db, program_type, Some(ifindex), direction, nsid)?;

    let mut members = Vec::with_capacity(links.len());
    for link in &links {
        let (priority, proceed_on) = match link {
            Link::Xdp(l) => (l.get_priority()?, l.get_proceed_on()?.mask()),
            Link::Tc(l) => (l.get_priority()?, l.get_proceed_on()?.mask()),
            _ => continue,
        };

        members.push(DispatcherMember {
            program_id: link.get_program_id()?,
            program_name: link.get_program_name()?,
            link_id: link.get_id()?,
            kernel_link_id: None,
            position: link.get_current_position()?,
            priority,
            proceed_on,
        });
    }

    // Present members in execution order (ascending slot position), the
    // order the kernel runs the chain.
    members.sort_by_key(|m| m.position.unwrap_or(usize::MAX));

    Ok(DispatcherSnapshot {
        key,
        revision,
        runtime,
        members,
    })
}
