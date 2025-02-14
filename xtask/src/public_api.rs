use std::{
    fmt::Write as _,
    fs::{read_to_string, File},
    io::Write as _,
    path::Path,
};

use anyhow::{bail, Context as _, Result};
use cargo_metadata::{Metadata, Package, Target};
use clap::Parser;
use dialoguer::{theme::ColorfulTheme, Confirm};
use diff::{lines, Result as Diff};

#[derive(Debug, Parser)]
pub struct Options {
    /// Optional: Bless new API changes [default: false].
    #[clap(long)]
    pub bless: bool,

    /// Optional: Rust target to run against [default: host].
    #[clap(long)]
    pub target: Option<String>,

    /// Optional: Override the rust toolchain.
    #[clap(long, default_value = "nightly")]
    pub toolchain: String,
}

pub fn public_api(options: Options, metadata: Metadata) -> Result<()> {
    let Options {
        bless,
        target,
        toolchain,
    } = options;

    if !rustup_toolchain::is_installed(&toolchain)? {
        if Confirm::with_theme(&ColorfulTheme::default())
            .with_prompt(
                format! {"No {toolchain} toolchain detected. Would you like to install one?"},
            )
            .interact()?
        {
            rustup_toolchain::install(&toolchain)?;
        } else {
            bail!(format! {"{toolchain} toolchain not installed"})
        }
    }

    let Metadata {
        workspace_root,
        packages,
        ..
    } = metadata;

    let errors: Vec<_> = packages
        .into_iter()
        .map(
            |Package {
                 name,
                 publish,
                 targets,
                 ..
             }| {
                if matches!(publish, Some(publish) if publish.is_empty())
                    || name == "bpfman-api"
                    || name == "bpfman-csi"
                {
                    Ok(())
                } else {
                    if !targets
                        .clone()
                        .into_iter()
                        .any(|target| target.kind.contains(&"lib".to_string()))
                    {
                        return Ok(());
                    }

                    let arch = target.as_ref().and_then(|target| {
                        let proc_macro = targets.iter().any(|Target { kind, .. }| {
                            kind.iter().any(|kind| kind == "proc-macro")
                        });
                        (!proc_macro).then_some(target)
                    });

                    let diff = match check_package_api(
                        &name,
                        &toolchain,
                        arch.cloned(),
                        bless,
                        workspace_root.as_std_path(),
                    ) {
                        Ok(d) => d,
                        Err(e) => {
                            return Err(anyhow::anyhow!("{name} failed to check public API:\n{e}"));
                        }
                    };
                    if diff.is_empty() {
                        Ok(())
                    } else {
                        Err(anyhow::anyhow!(
                            "{name} public API changed; re-run with --bless. diff:\n{diff}"
                        ))
                    }
                }
            },
        )
        .filter_map(|result| result.err())
        .collect();

    if errors.is_empty() {
        Ok(())
    } else {
        for error in errors {
            eprintln!("{}", error);
        }
        bail!("public API generation failed")
    }
}

fn check_package_api(
    package: &str,
    toolchain: &str,
    target: Option<String>,
    bless: bool,
    workspace_root: &Path,
) -> Result<String> {
    let path = workspace_root
        .join("xtask")
        .join("public-api")
        .join(package)
        .with_extension("txt");

    let mut builder = rustdoc_json::Builder::default()
        .toolchain(toolchain)
        .package(package)
        .all_features(true);
    if let Some(target) = target {
        builder = builder.target(target);
    }
    let rustdoc_json = builder.build().with_context(|| {
        format!(
            "rustdoc_json::Builder::default().toolchain({}).package({}).build()",
            toolchain, package
        )
    })?;

    let public_api = match public_api::Builder::from_rustdoc_json(&rustdoc_json).build() {
        Ok(pa) => pa,
        Err(e) => {
            return Err(anyhow::anyhow!(
                "public_api::Builder::from_rustdoc_json({})::build():\n{e}",
                rustdoc_json.display()
            ));
        }
    };

    if bless {
        let mut output =
            File::create(&path).with_context(|| format!("error creating {}", path.display()))?;
        write!(&mut output, "{}", public_api)
            .with_context(|| format!("error writing {}", path.display()))?;
    }
    let current_api =
        read_to_string(&path).with_context(|| format!("error reading {}", path.display()))?;

    Ok(lines(&public_api.to_string(), &current_api)
        .into_iter()
        .fold(String::new(), |mut buf, diff| {
            match diff {
                Diff::Both(..) => (),
                Diff::Right(line) => writeln!(&mut buf, "-{}", line).unwrap(),
                Diff::Left(line) => writeln!(&mut buf, "+{}", line).unwrap(),
            };
            buf
        }))
}
