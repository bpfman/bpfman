#!/bin/bash

# test_xdp_container_restart.sh
# This test simulates a node reboot scenario, ensuring that the database is correctly reset.
# It builds a local container image and runs runs xdp_pass locally. The container is then committed, stopped and
# a new instance is started with the commit.
# The bpfman commands are executed inside the container to ensure that programs can be loaded and attached correctly after the restart.

set -euo pipefail

# Constants
readonly IMAGE_NAME="${IMAGE_NAME:-localhost/bpfman:test}"
readonly CONTAINERFILE="${CONTAINERFILE:-./Containerfile.bpfman.local}"
readonly CONTAINER_NAME="${CONTAINER_NAME:-bpfman_xdp_test}"
readonly BPFMAN_LOAD_CMD="${BPFMAN_LOAD_CMD:-bpfman load image --image-url quay.io/bpfman-bytecode/xdp_pass:latest --programs xdp:pass --application XdpPassProgram}"
readonly OCI_BIN="${OCI_BIN:-docker}"

# Cleanup function
cleanup() {
    echo "Current status"
    ${OCI_BIN} ps -a
    ${OCI_BIN} images

    echo "Cleaning up containers and images..."
    "${OCI_BIN}" stop "${CONTAINER_NAME}" 2>/dev/null || true
    "${OCI_BIN}" rm "${CONTAINER_NAME}" 2>/dev/null || true
    "${OCI_BIN}" stop "$(get_commit_name "${CONTAINER_NAME}")" 2>/dev/null || true
    "${OCI_BIN}" rm "$(get_commit_name "${CONTAINER_NAME}")" 2>/dev/null || true
    "${OCI_BIN}" rmi "${IMAGE_NAME}" 2>/dev/null || true
    "${OCI_BIN}" rmi "$(get_commit_name "${IMAGE_NAME}")" 2>/dev/null || true
}

# Function to get container ID
get_container_id() {
    local name="$1"
    "${OCI_BIN}" inspect "${name}" -f "{{.Id}}" 2>/dev/null || echo ""
}

# Function to build container image
build_container_image() {
    local image_name="$1"
    local containerfile="$2"

    if ! "${OCI_BIN}" build -f "${containerfile}" . --tag "${image_name}" >&2; then
        echo "Failed to build image ${image_name} from container file ${containerfile} with ${OCI_BIN}" >&2
        exit 1
    fi

    local image_id
    image_id=$(get_container_id "${image_name}")
    if [[ -z "${image_id}" ]]; then
        echo "Failed to get image id for ${image_name}" >&2
        exit 1
    fi

    echo "${image_id}"
}

# Function to run new container
run_new_container() {
    local container_name="$1"
    local image_id="$2"
    local privileged="$3"    # true/false
    local entrypoint="$4"    # Optional, can be empty
    shift 4
    local command=("$@")     # Remaining arguments as command array

    local args=("run" "--name" "${container_name}")

    # Add privileged if true
    if [[ "${privileged}" == "true" ]]; then
        args+=("--privileged")
    fi

    # Add entrypoint if provided
    if [[ -n "${entrypoint}" ]]; then
        # EMPTY is a 'sentinel value' as $entrypoint needs to handle 3 cases:
        # 1) not appending --entrypoint
        # 2) appending --entrypoint "" (to reset preconfigured entrypoint)
        # 3) appending --entrypoint "${value}"
        if [[ "${entrypoint}" == "EMPTY" ]]; then
            args+=("--entrypoint" "")
        else
            args+=("--entrypoint" "${entrypoint}")
        fi
    fi

    # Add tmpfs mount
    args+=("--mount" "type=tmpfs,dst=/run")

    # Add detached mode and image
    args+=("-d" "${image_id}")

    # Add command if provided
    if [[ ${#command[@]} -gt 0 ]]; then
        args+=("${command[@]}")
    fi

    local container_id
    container_id=$("${OCI_BIN}" "${args[@]}")

    if [[ -z "${container_id}" ]]; then
        echo "Failed to run container ${container_name} with ${OCI_BIN}, got no container id" >&2
        exit 1
    fi

    echo "${container_id}"
}

# Create the commit name from a provided name
get_commit_name() {
    local name="$1"
    echo "${name}_commit"
}

# Function to execute command in container
exec_in_container() {
    local container_id="$1"
    local cmd="$2"

    local output
    if ! output=$("${OCI_BIN}" exec "${container_id}" bash -c "${cmd}" 2>&1); then
        echo "Command failed: ${output}" >&2
        exit 1
    fi

    echo "${output}"
}

# Function to commit container to image
commit_container() {
    local container_id="$1"
    local new_image_name="$2"

    if ! output=$("${OCI_BIN}" commit "${container_id}" "${new_image_name}" 2>&1); then
        echo "Failed to commit container: ${output}" >&2
        exit 1
    fi

    echo "${output}"
}

start_docker() {
    echo "Starting docker service..."
    if ! systemctl start docker; then
        echo "Failed to start docker service" >&2
        exit 1
    fi

    timeout="${DOCKER_TIMEOUT:-30}"
    while [ "$timeout" -gt 0 ]; do
        echo "Waiting for docker daemon to be ready... ($timeout)"
        if docker info >/dev/null 2>&1; then
            echo "Docker daemon is ready"
            return
        fi
        sleep 1
        ((timeout--))
    done

    echo "Docker daemon failed to become ready within ${DOCKER_TIMEOUT:-30} seconds" >&2
    exit 1
}

# Main test function
main() {
    echo "Starting XDP container restart test..."

    # Start OCI service (docker/podman)
    if [[ "${OCI_BIN}" == "docker" ]]; then
        if ! systemctl is-active --quiet docker; then
            start_docker
        fi
    fi

    # Set trap for cleanup
    trap cleanup EXIT

    # Clean up at start, just in case
    cleanup

    # Build container image
    local current_image_id
    current_image_id=$(build_container_image "${IMAGE_NAME}" "${CONTAINERFILE}")
    current_container_name="${CONTAINER_NAME}"

    # Test iterations: with and without commit
    local iterations=(true false)
    for should_commit in "${iterations[@]}"; do
        echo -e "\n=== Iteration with should_commit=${should_commit} ==="

        # Run new container
        echo "Starting container ${current_container_name} with image ${current_image_id}"
        local current_container_id
        current_container_id=$(run_new_container "${current_container_name}" "${current_image_id}" "true" "EMPTY" "sleep" "infinity")

        # Load BPF program
        echo "Loading BPF program in container ${current_container_id}: ${BPFMAN_LOAD_CMD}"
        local output
        output=$(exec_in_container "${current_container_id}" "${BPFMAN_LOAD_CMD}")

        # Extract program ID from output
        local program_id
        program_id=$(echo "${output}" | grep "Program ID:" | awk '{print $3}')
        if [[ -z "${program_id}" ]]; then
            echo "Failed to find program ID in output: ${output}" >&2
            exit 1
        fi
        echo "Found program ID: ${program_id}"

        # Attach program to interface
        local attach_cmd="bpfman attach ${program_id} xdp --iface eth0 --priority 100"
        echo "Attaching program ${program_id} to eth0: ${attach_cmd}"
        exec_in_container "${current_container_id}" "${attach_cmd}"

        echo "Program attached successfully"

        # Commit container if requested
        if [[ "${should_commit}" == "true" ]]; then
            commit_name="$(get_commit_name "${current_container_name}")"
            echo "Committing container ${current_container_id} to image name ${commit_name}"
            current_image_id=$(commit_container "${current_container_id}" "${commit_name}")
            current_container_name="${commit_name}"
        fi

        # Stop and remove container for next iteration
        echo "Stopping and removing container ${current_container_id}"
        "${OCI_BIN}" stop "${current_container_id}"
        "${OCI_BIN}" rm "${current_container_id}"
    done

    echo -e "\n=================================================="
    echo "XDP container restart test completed successfully!"
    echo "=================================================="
}

# Run the test
main "$@"
