#!/usr/bin/env bash

# Script to generate RPM lockfile using rpm-lockfile-prototype container
# Based on: https://github.com/konflux-ci/rpm-lockfile-prototype

set -euo pipefail

container_name="localhost/rpm-lockfile-prototype"
rpms_in_file="rpms.in.yaml"
rpms_lock_file="rpms.lock.yaml"
default_base_image="registry.access.redhat.com/ubi9/ubi-minimal:latest"

print_status() {
    echo "[INFO] $1"
}

print_success() {
    echo "[SUCCESS] $1"
}

print_warning() {
    echo "[WARNING] $1"
}

print_error() {
    echo "[ERROR] $1"
}

usage() {
    cat << EOF
Usage: ${0##*/} [OPTIONS]

Generate RPM lockfile using rpm-lockfile-prototype container.

OPTIONS:
    -i, --input FILE        Input rpms.in.yaml file (default: ${rpms_in_file})
    -o, --output FILE       Output rpms.lock.yaml file (default: ${rpms_lock_file})
    -b, --base-image IMAGE  Base container image (default: ${default_base_image})
    --rebuild-container     Force rebuild of rpm-lockfile-prototype container
    -h, --help              Show this help message

EXAMPLES:
    ${0##*/}                                          # Use default files and base image
    ${0##*/} -b registry.access.redhat.com/ubi9/python-312  # Use different base image
    ${0##*/} --rebuild-container                      # Force container rebuild
    ${0##*/} -i my-rpms.in.yaml -o my-rpms.lock.yaml # Use custom input/output files

REQUIREMENTS:
    - podman must be installed and available
    - ${rpms_in_file} must exist in current directory
    - Internet connection for downloading container images

EOF
}

check_requirements() {
    print_status "Checking requirements..."

    if ! command -v podman &> /dev/null; then
        print_error "podman is required but not installed"
        exit 1
    fi

    if [[ ! -f "$rpms_in_file" ]]; then
        print_error "Input file $rpms_in_file not found"
        print_status "Create a $rpms_in_file file with the following format:"
        cat << 'EOF'
packages:
  - diffutils
contentOrigin:
  repofiles:
    - ./ubi.repo
arches:
  - x86_64
context:
  containerfile:
    file: Containerfile.bundle.openshift
    stageName: builder
EOF
        exit 1
    fi

    print_success "Requirements check passed"
}

build_container() {
    local rebuild_flag=${1:-false}
    local temp_dir=$2

    if podman image exists "$container_name" && [[ "$rebuild_flag" != "true" ]]; then
        print_status "Container $container_name already exists, skipping build"
        print_status "Use --rebuild-container to force rebuild"
        return 0
    fi

    print_status "Building rpm-lockfile-prototype container..."

    print_status "Cloning rpm-lockfile-prototype repository..."
    if ! git clone -q https://github.com/konflux-ci/rpm-lockfile-prototype.git "$temp_dir/rpm-lockfile-prototype"; then
        print_error "Failed to clone rpm-lockfile-prototype repository"
        exit 1
    fi

    print_status "Building container image..."
    if ! podman build -f "$temp_dir/rpm-lockfile-prototype/Containerfile" -t "$container_name" "$temp_dir/rpm-lockfile-prototype" --quiet; then
        print_error "Failed to build container"
        exit 1
    fi

    print_success "Container built successfully: $container_name"
}

generate_lockfile() {
    local base_image="$1"
    local input_file="$2"
    local output_file="$3"

    print_status "Generating RPM lockfile..."
    print_status "Base image: $base_image"
    print_status "Input file: $input_file"
    print_status "Output file: $output_file"

    if [[ -f "$output_file" ]]; then
        local backup_file
        backup_file="${output_file}.backup.$(date +%Y%m%d-%H%M%S)"
        cp "$output_file" "$backup_file"
        print_warning "Backed up existing $output_file to $backup_file"
    fi

    print_status "Running rpm-lockfile-prototype..."
    if ! podman run --rm \
         -v "$(pwd):/work:Z" \
         -w /work \
         "$container_name" \
         --image "$base_image" \
         --outfile "$output_file" \
         "$input_file"; then
        print_error "Failed to generate lockfile"
        exit 1
    fi

    if [[ -f "$output_file" ]]; then
        print_success "RPM lockfile generated successfully: $output_file"

        local package_count
        package_count=$(grep -c "name:" "$output_file" || echo "0")
        print_status "Generated lockfile contains $package_count packages"

        if [[ $package_count -gt 0 ]]; then
            print_status "Sample packages in lockfile:"
            grep "name:" "$output_file" | head -5 | sed 's/^/  /'
            if [[ $package_count -gt 5 ]]; then
                print_status "  ... and $((package_count - 5)) more"
            fi
        fi
    else
        print_error "Lockfile was not generated"
        exit 1
    fi
}

validate_lockfile() {
    local output_file="$1"

    print_status "Validating generated lockfile..."

    if ! grep -q "lockfileVersion:" "$output_file"; then
        print_error "Generated file does not appear to be a valid RPM lockfile"
        exit 1
    fi

    if ! grep -q "packages:" "$output_file"; then
        print_warning "Lockfile contains no packages - this may be expected if all packages are already in the base image"
    fi

    print_success "Lockfile validation passed"
}

base_image="$default_base_image"
input_file="$rpms_in_file"
output_file="$rpms_lock_file"
rebuild_container=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -i|--input)
            input_file="$2"
            shift 2
            ;;
        -o|--output)
            output_file="$2"
            shift 2
            ;;
        -b|--base-image)
            base_image="$2"
            shift 2
            ;;
        --rebuild-container)
            rebuild_container=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

print_status "Starting RPM lockfile generation..."
print_status "Working directory: $(pwd)"

temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

check_requirements
build_container "$rebuild_container" "$temp_dir"
generate_lockfile "$base_image" "$input_file" "$output_file"
validate_lockfile "$output_file"

print_success "RPM lockfile generation completed!"
print_status "Next steps:"
print_status "  1. Review the generated $output_file"
print_status "  2. Commit both $input_file and $output_file to your repository"
print_status "  3. Ensure your Tekton pipelines have the correct prefetch-input configuration"
