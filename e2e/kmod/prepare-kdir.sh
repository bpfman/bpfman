#!/usr/bin/env bash
set -euo pipefail

kernel_release=${KERNEL_RELEASE:-$(uname -r)}
kernel_mod_dir_version=${KERNEL_MOD_DIR_VERSION:-$kernel_release}
default_kdir="/lib/modules/${kernel_release}/build"
kdir=${KDIR:-$default_kdir}
kernel_dev=${KERNEL_DEV:-}
kbuild=${E2E_KMOD_KBUILD:-e2e/kmod/.kbuild}
if [[ "$kbuild" != /* ]]; then
	kbuild="$(pwd -P)/$kbuild"
fi

find_nixos_kernel_dev() {
	local kernel_image kernel_out kernel_drv output

	command -v nix-store >/dev/null 2>&1 || return 1
	[[ -e /run/current-system/kernel ]] || return 1

	kernel_image=$(readlink -f /run/current-system/kernel) || return 1
	kernel_out=${kernel_image%/bzImage}
	[[ -e "$kernel_out" ]] || return 1

	kernel_drv=$(nix-store -q --deriver "$kernel_out" 2>/dev/null) || return 1
	[[ "$kernel_drv" == *.drv ]] || return 1

	while IFS= read -r output; do
		if [[ "$output" == *-linux-"${kernel_release}"-dev ]]; then
			if [[ ! -e "$output" ]]; then
				nix-store -r "$output" >/dev/null
			fi
			printf '%s\n' "$output"
			return 0
		fi
	done < <(nix-store -q --outputs "$kernel_drv" 2>/dev/null)

	return 1
}

prepare_kernel_dev() {
	local kernel_build=$1

	if [[ ! -d "$kernel_build" ]]; then
		{
			echo "error: kernel build tree not found: $kernel_build"
			echo "Set KERNEL_MOD_DIR_VERSION=... if the module directory version differs from uname -r."
		} >&2
		exit 1
	fi

	if [[ -d "$kbuild" ]]; then
		find "$kbuild" -type d -exec chmod u+w {} +
		rm -rf "$kbuild"
	fi
	mkdir -p "$kbuild"
	cp -rs "$kernel_build"/. "$kbuild"/
	find "$kbuild" -type d -exec chmod u+w {} +
	for generated in Makefile Module.symvers; do
		if [[ -L "$kbuild/$generated" ]]; then
			cp --remove-destination "$kernel_build/$generated" "$kbuild/$generated"
			chmod u+w "$kbuild/$generated"
		fi
	done
	local kernel_source=""
	if [[ -e "$kernel_build/source" ]]; then
		kernel_source=$(readlink -f "$kernel_build/source")
		local kernel_build_real
		kernel_build_real=$(readlink -f "$kernel_build")
		if [[ -d "$kernel_source" && "$kernel_source" != "$kernel_build_real" ]]; then
			printf '%s\n' "$kernel_source" >"$kbuild/.kernel-source"
		fi
	fi

	# Seed vmlinux for the kbuild BTF [M] step. Without it kbuild
	# silently prints "Skipping BTF generation ... due to
	# unavailability of vmlinux" and the resulting .ko has no .BTF
	# section; then libbpf cannot resolve module functions to a BTF
	# ID and fentry/fexit attach fails at load time with a cryptic
	# "not supported" from the kernel. We never want the silent
	# path: if no vmlinux source is reachable, fail here with a
	# clear diagnostic.
	local vmlinux_src=""
	if [[ -n "$kernel_dev" && -e "${kernel_dev}/vmlinux" ]]; then
		vmlinux_src=${kernel_dev}/vmlinux
	elif [[ -e /sys/kernel/btf/vmlinux ]]; then
		vmlinux_src=/sys/kernel/btf/vmlinux
	fi

	if [[ -z "$vmlinux_src" ]]; then
		{
			echo "error: vmlinux required for module BTF generation, but none found"
			if [[ -n "$kernel_dev" ]]; then
				echo "  checked: ${kernel_dev}/vmlinux (absent)"
			fi
			echo "  checked: /sys/kernel/btf/vmlinux (absent)"
			echo ""
			echo "Without a vmlinux that kbuild can read, the kmod has no"
			echo ".BTF section, and fentry/fexit attach to its functions"
			echo "fails at load time. On hosts running a"
			echo "CONFIG_DEBUG_INFO_BTF=y kernel, /sys/kernel/btf/vmlinux"
			echo "is the expected source; otherwise pass KERNEL_DEV=<path>"
			echo "with <path>/vmlinux pointing at a vmlinux ELF or BTF blob."
		} >&2
		exit 1
	fi

	ln -sf "$vmlinux_src" "$kbuild/vmlinux"

	printf '%s\n' "$kbuild"
}

if [[ -d "$kdir" ]]; then
	# Fast path: a conventional Fedora/Ubuntu kdir is usable as-is
	# ONLY if it already carries a vmlinux that pahole can read
	# during the kbuild BTF [M] step. Distro `linux-headers`
	# packages typically omit vmlinux (Ubuntu's noble linux-headers
	# is the motivating case), so check for it and fall through to
	# the mirror path when missing -- prepare_kernel_dev will seed
	# a vmlinux symlink from /sys/kernel/btf/vmlinux (or the
	# user-supplied $KERNEL_DEV/vmlinux) so kbuild generates BTF
	# for the .ko rather than emitting "Skipping BTF generation
	# ... due to unavailability of vmlinux".
	if [[ -e "$kdir/vmlinux" ]]; then
		printf '%s\n' "$kdir"
		exit 0
	fi
	prepare_kernel_dev "$kdir"
	exit 0
fi

if [[ -n "$kernel_dev" ]]; then
	prepare_kernel_dev "${kernel_dev}/lib/modules/${kernel_mod_dir_version}/build"
	exit 0
fi

if kernel_dev=$(find_nixos_kernel_dev); then
	kernel_mod_dir_version=$kernel_release
	prepare_kernel_dev "${kernel_dev}/lib/modules/${kernel_mod_dir_version}/build"
	exit 0
fi

{
	echo "error: KDIR=$kdir does not exist"
	echo "Install matching kernel headers/build tree or pass KDIR=..."
	echo "On NixOS, make sure the current kernel derivation is still available, or pass KERNEL_DEV=..."
} >&2
exit 1
