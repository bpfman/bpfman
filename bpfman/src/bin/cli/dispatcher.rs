// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of bpfman

use anyhow::anyhow;
use bpfman::{
    dispatcher_view::{
        DispatcherSnapshot, DispatcherSummary, get_dispatcher_snapshot, list_dispatcher_snapshots,
    },
    setup,
};
use comfy_table::{Cell, Table, presets::NOTHING};

use crate::args::{DispatcherGetArgs, DispatcherListArgs, DispatcherSubcommand, OutputFormat};

impl DispatcherSubcommand {
    pub(crate) fn execute(&self) -> anyhow::Result<()> {
        match self {
            DispatcherSubcommand::List(args) => execute_dispatcher_list(args),
            DispatcherSubcommand::Get(args) => execute_dispatcher_get(args),
        }
    }
}

fn execute_dispatcher_list(args: &DispatcherListArgs) -> anyhow::Result<()> {
    let (_, root_db) = setup()?;
    let summaries =
        list_dispatcher_snapshots(&root_db).map_err(|e| anyhow!("dispatcher list error: {e}"))?;

    match args.output {
        // Wrap in a "dispatchers" object to match the Go CLI's list JSON.
        OutputFormat::Json => println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({ "dispatchers": summaries }))?
        ),
        OutputFormat::Text => print_summary_table(&summaries),
    }
    Ok(())
}

fn execute_dispatcher_get(args: &DispatcherGetArgs) -> anyhow::Result<()> {
    let (_, root_db) = setup()?;
    let snapshot =
        get_dispatcher_snapshot(&root_db, &args.dispatcher_type, args.nsid, args.ifindex)
            .map_err(|e| anyhow!("dispatcher get error: {e}"))?;

    let snapshot = match snapshot {
        Some(s) => s,
        None => {
            return Err(anyhow!(
                "dispatcher ({}, {}, {}) not found",
                args.dispatcher_type,
                args.nsid,
                args.ifindex
            ));
        }
    };

    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&snapshot)?),
        OutputFormat::Text => print_snapshot_table(&snapshot),
    }
    Ok(())
}

fn print_summary_table(summaries: &[DispatcherSummary]) {
    let mut table = Table::new();
    table.load_preset(NOTHING);
    table.set_header(vec![
        "TYPE", "NSID", "IFINDEX", "REVISION", "PRIORITY", "HANDLE", "MEMBERS", "NETNS",
    ]);

    for s in summaries {
        table.add_row(vec![
            Cell::new(&s.key.dispatcher_type),
            Cell::new(s.key.nsid),
            Cell::new(s.key.ifindex),
            Cell::new(s.revision),
            Cell::new(opt_display(s.runtime.filter_priority)),
            Cell::new(
                s.runtime
                    .filter_handle
                    .map(|h| format!("{h:#x}"))
                    .unwrap_or_else(|| "-".to_string()),
            ),
            Cell::new(s.member_count),
            Cell::new(if s.runtime.netns_path.is_empty() {
                "-"
            } else {
                &s.runtime.netns_path
            }),
        ]);
    }
    println!("{table}");
}

fn print_snapshot_table(snapshot: &DispatcherSnapshot) {
    println!(
        "Dispatcher: {} nsid={} ifindex={}",
        snapshot.key.dispatcher_type, snapshot.key.nsid, snapshot.key.ifindex
    );
    println!("  Revision:       {}", snapshot.revision);
    println!(
        "  Program ID:     {}",
        opt_display(snapshot.runtime.program_id)
    );
    println!(
        "  Kernel Link ID: {}",
        opt_display(snapshot.runtime.kernel_link_id)
    );

    println!("\nMembers ({}):", snapshot.members.len());
    if snapshot.members.is_empty() {
        println!("  (none)");
        return;
    }

    let mut table = Table::new();
    table.load_preset(NOTHING);
    table.set_header(vec![
        "POS",
        "PRIORITY",
        "PROGRAM ID",
        "FUNCTION NAME",
        "LINK ID",
        "KERNEL LINK ID",
        "PROCEED ON",
    ]);

    for m in &snapshot.members {
        table.add_row(vec![
            Cell::new(opt_display(m.position)),
            Cell::new(m.priority),
            Cell::new(m.program_id),
            Cell::new(&m.program_name),
            Cell::new(m.link_id),
            Cell::new(opt_display(m.kernel_link_id)),
            Cell::new(m.proceed_on),
        ]);
    }
    println!("{table}");
}

fn opt_display<T: std::fmt::Display>(v: Option<T>) -> String {
    match v {
        Some(v) => v.to_string(),
        None => "-".to_string(),
    }
}
