# SPDX-License-Identifier: GPL-2.0
#
# Out-of-tree kbuild for bpfman_e2e_targets.ko. Drives the
# kernel build system via M= so the module is compiled against
# the headers of the running kernel by default. Override KDIR
# to build against a different kernel tree (cross-compile, CI
# pinned kernel, etc.).

obj-m += bpfman_e2e_targets.o
ccflags-y += -I$(src)

KDIR ?= /lib/modules/$(shell uname -r)/build
PWD  := $(shell pwd)

.PHONY: all clean

all:
	$(MAKE) -C $(KDIR) $(if $(KBUILD_OUTPUT),O=$(KBUILD_OUTPUT)) M=$(PWD) modules

clean:
	$(MAKE) -C $(KDIR) $(if $(KBUILD_OUTPUT),O=$(KBUILD_OUTPUT)) M=$(PWD) clean
