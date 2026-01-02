# SPDX-License-Identifier: Apache-2.0
# Copyright Authors of bpfman

# Makefile for eBPF dispatcher compilation.

LIBBPF_CFLAGS := $(shell pkg-config --cflags libbpf)

# Dynamic architecture detection using rustc (consistent with CARGO_CFG_TARGET_ARCH)
TARGET_ARCH ?= $(shell rustc --print cfg | grep target_arch | cut -d'"' -f2)

ifeq ($(TARGET_ARCH),x86_64)
  TARGET_ARCH_DEFINE := -D__TARGET_ARCH_x86
else ifeq ($(TARGET_ARCH),i686)
  TARGET_ARCH_DEFINE := -D__TARGET_ARCH_x86
else ifeq ($(TARGET_ARCH),aarch64)
  TARGET_ARCH_DEFINE := -D__TARGET_ARCH_arm64
else ifeq ($(TARGET_ARCH),powerpc64le)
  TARGET_ARCH_DEFINE := -D__TARGET_ARCH_powerpc
else ifeq ($(TARGET_ARCH),s390x)
  TARGET_ARCH_DEFINE := -D__TARGET_ARCH_s390
else
  $(error Unsupported architecture: $(TARGET_ARCH))
endif

SOURCES := $(wildcard *.bpf.c)
# Use OUT_DIR if provided, otherwise use current working directory.
OUT_DIR ?= $(PWD)
OBJECTS := $(addprefix $(OUT_DIR)/,$(SOURCES:.bpf.c=.bpf.o))
DEP_FILES := $(addprefix $(OUT_DIR)/,$(SOURCES:.bpf.c=.bpf.d))

all: $(OBJECTS)

$(OUT_DIR)/%.bpf.o: %.bpf.c Makefile | $(OUT_DIR)
	clang $(LIBBPF_CFLAGS) -g -O2 -target bpfel -c $(TARGET_ARCH_DEFINE) -MD -MP -MF$(OUT_DIR)/$*.bpf.d $< -o $@

$(OUT_DIR):
	mkdir -p $(OUT_DIR)

.PHONY: clean
clean:
	rm -f $(OBJECTS) $(DEP_FILES)

.PHONY: test-pkgconfig
test-pkgconfig:
	pkg-config --exists libbpf

.PHONY: debug
debug:
	@echo "TARGET_ARCH: $(TARGET_ARCH)"
	@echo "TARGET_ARCH_DEFINE: $(TARGET_ARCH_DEFINE)"
	@echo "LIBBPF_CFLAGS: $(LIBBPF_CFLAGS)"
	@which clang
	@ls -la *.bpf.c

-include $(DEP_FILES)
