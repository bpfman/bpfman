use std::process::Command;

use anyhow::{Result, bail};

#[derive(Debug)]
pub struct DockerContainer {
    name: String,
    pid: i32,
    id: String,
}

impl Drop for DockerContainer {
    fn drop(&mut self) {
        // Only drop if we actually have an id (i.e. the container was created / retrieved)
        // We do not want to clean up too early.
        if self.id.is_empty() {
            return;
        }
        match self.stop() {
            Ok(_) => println!(
                "Docker container {} ({}) stopped successfully",
                self.name, self.id
            ),
            Err(e) => println!(
                "Error stopping container {} ({}): {}",
                self.name, self.id, e
            ),
        }
        match self.remove() {
            Ok(_) => println!(
                "Docker container {} ({}) removed successfully",
                self.name, self.id
            ),
            Err(e) => println!(
                "Error removing container {} ({}): {}",
                self.name, self.id, e
            ),
        }
    }
}

impl DockerContainer {
    pub fn new(name: &str) -> Result<Self> {
        let id = get_docker_id(name)?;
        let pid = get_container_pid(name)?;

        Ok(DockerContainer {
            id,
            pid,
            name: name.to_string(),
        })
    }

    // id returns the id of the container if it is set, otherwise it returns the name
    pub fn id(&self) -> &str {
        if !self.id.is_empty() {
            return &self.id;
        }
        &self.name
    }

    /// Return the container PID
    pub fn pid(&self) -> i32 {
        self.pid
    }

    pub fn remove(&self) -> Result<()> {
        let output = Command::new("docker").args(["rm", self.id()]).output()?;
        handle_docker_output(output, "container removal")
    }

    pub fn stop(&self) -> Result<()> {
        let output = Command::new("docker").args(["stop", self.id()]).output()?;
        handle_docker_output(output, "container stop")
    }

    pub fn commit(&self, new_image_name: &str) -> Result<DockerContainerImage> {
        let output = Command::new("docker")
            .args(["commit", self.id(), new_image_name])
            .output()?;
        handle_docker_output(output, "container commit")?;

        let image_id = get_docker_id(new_image_name)?;
        if image_id.is_empty() {
            bail!("failed to get image id for {new_image_name}");
        }

        println!("Docker container committed to image {new_image_name} with ID {image_id}");
        Ok(DockerContainerImage {
            id: image_id,
            name: new_image_name.to_string(),
        })
    }

    // Runs a command in the container and returns the output
    pub fn exec(&self, cmd: &str) -> Result<String> {
        println!("Running command inside docker container: {cmd}");

        let output = Command::new("docker")
            .args(["exec", self.id(), "bash", "-c", cmd])
            .output()?;

        if !output.status.success() {
            bail!(
                "Command failed with status {}: {}",
                output.status,
                String::from_utf8_lossy(&output.stderr)
            );
        }

        println!("stdout: {}", String::from_utf8_lossy(&output.stdout));
        println!("stderr: {}", String::from_utf8_lossy(&output.stderr));

        Ok(String::from_utf8(output.stdout)?)
    }
}

#[derive(Debug, Clone)]
pub struct DockerContainerImage {
    id: String,
    name: String,
}

impl DockerContainerImage {
    pub fn new(name: &str) -> Result<Self> {
        let id = get_docker_id(name)?;

        Ok(DockerContainerImage {
            id,
            name: name.to_string(),
        })
    }

    // id returns the id of the image if it is set, otherwise it returns the name
    pub fn id(&self) -> &str {
        if !self.id.is_empty() {
            return &self.id;
        }
        &self.name
    }

    pub fn remove(&self) -> Result<()> {
        let output = Command::new("docker").args(["rmi", self.id()]).output()?;
        handle_docker_output(output, "image removal")
    }
}

impl Drop for DockerContainerImage {
    fn drop(&mut self) {
        // Only drop if we actually have an id (i.e. the image was built / retrieved)
        // We do not want to clean up too early.
        if self.id.is_empty() {
            return;
        }
        match self.remove() {
            Ok(_) => println!(
                "Docker container image {} ({}) removed successfully",
                self.name, self.id
            ),
            Err(e) => println!(
                "Error removing container image {} ({}): {}",
                self.name, self.id, e
            ),
        }
    }
}

/// Starts the docker service if it is not already running
pub fn start_docker() -> Result<()> {
    let status = Command::new("systemctl")
        .args(["start", "docker"])
        .status()?;
    if !status.success() {
        bail!("failed to start docker service, status: {status:?}");
    }
    Ok(())
}
/// Starts a docker container from the nginx image
pub fn run_new_container(
    container_name: &str,
    image_name: &str,
    port_mapping: Option<&str>,
    privileged: bool,
    entrypoint: Option<&str>,
    command: Option<Vec<&str>>,
) -> Result<DockerContainer> {
    let mut args = vec!["run", "--name", container_name];
    if let Some(mapping) = port_mapping {
        args.extend(["-p", mapping]);
    }
    if privileged {
        args.push("--privileged");
    }
    if let Some(epoint) = entrypoint {
        args.extend(["--entrypoint", epoint]);
    }
    args.extend(["--mount", "type=tmpfs,dst=/run"]);
    args.extend(["-d", image_name]);
    if let Some(cmd) = command {
        args.extend(cmd);
    }
    println!("Running docker command with {args:?}");
    let output = Command::new("docker").args(args).output()?;
    let id = String::from_utf8(output.stdout)?.trim().to_string();
    if id.is_empty() {
        bail!("failed to run docker container, got no container id");
    }

    let pid = get_container_pid(container_name)?;
    if pid == 0 {
        bail!("failed to run docker container, got no container pid");
    }

    println!("Docker container {container_name} with ID {id} and PID {pid} created",);

    Ok(DockerContainer {
        name: container_name.to_string(),
        pid,
        id,
    })
}

pub fn build_container_image(
    image_name: &str,
    containerfile: &str,
) -> Result<DockerContainerImage> {
    let status = Command::new("docker")
        .args(["build", "-f", containerfile, ".", "--tag", image_name])
        .status()?;
    if !status.success() {
        bail!("failed to build docker image, status: {status}");
    }

    let image_id = get_docker_id(image_name)?;
    if image_id.is_empty() {
        bail!("failed get image id for {image_name}");
    }

    println!("Docker image built successfully");
    Ok(DockerContainerImage {
        id: image_id.to_string(),
        name: image_name.to_string(),
    })
}

fn get_docker_id(name: &str) -> Result<String> {
    let output = Command::new("docker")
        .args(["inspect", name, "-f", "{{.Id}}"])
        .output()?;
    let id = String::from_utf8(output.stdout)?.trim().to_string();
    Ok(id)
}

fn get_container_pid(name: &str) -> Result<i32> {
    let output = Command::new("docker")
        .args(["inspect", name, "-f", "{{.State.Pid}}"])
        .output()?;
    let pid = String::from_utf8(output.stdout)?
        .trim()
        .parse::<i32>()
        .unwrap_or(0);
    Ok(pid)
}

fn handle_docker_output(output: std::process::Output, operation: &str) -> Result<()> {
    if !output.status.success() && !String::from_utf8_lossy(&output.stderr).contains("No such") {
        bail!(
            "failed during docker {operation} with {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    Ok(())
}
