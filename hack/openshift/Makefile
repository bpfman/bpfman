# Sync VERSION from Cargo.toml to all OpenShift Containerfiles. Run this
# after merging from upstream or when VERSION changes in Cargo.toml.
.PHONY: set-version
set-version:
	@echo "Syncing Cargo.toml VERSION to OpenShift Containerfiles..."
	@./sync-version.sh

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  set-version  - Sync VERSION from Cargo.toml to OpenShift Containerfiles"