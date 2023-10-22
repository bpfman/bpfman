#!/bin/bash
# This script deletes the clsact and ingress qdiscs (if they exist) on all
# interfaces.  This has the side-effect of removing any tc_dispatchers (or other
# programs) that happen to be attached to them.  This is intended to be part of
# completely cleaning up the bpfd state on a system.  Caution should be used if
# other applications are currently using these qdiscs for other purposes.

interfaces=()
for iface in $(ifconfig | cut -d ' ' -f1| tr ':' '\n' | awk NF)
do
        interfaces+=("$iface")
done

for i in "${interfaces[@]}";
do
	# delete the clsact qdisc if it exists
	sudo tc qdisc del dev $i clsact 2>/dev/null;
	# delete the ingress qdisc if it exists
	sudo tc qdisc del dev $i ingress 2>/dev/null;
done
