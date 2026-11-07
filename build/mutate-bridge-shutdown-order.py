#!/usr/bin/env python3
"""Restore only old shutdown sequencing in a disposable source archive."""
import sys
from pathlib import Path

if len(sys.argv) != 2:
    raise SystemExit("expected the disposable archive's run.go path")
path = Path(sys.argv[1])
data = path.read_text(encoding="utf-8")
new = """func shutdownChildren(children map[string]*childServer) {
	for _, child := range children {
		child.input.Close()
	}
	stopChildren(children)
	for _, child := range children {
		child.output.Close()
	}
}"""
old = """func shutdownChildren(children map[string]*childServer) {
	for _, child := range children {
		child.input.Close()
		child.output.Close()
	}
	stopChildren(children)
}"""
if data.count(new) != 1 or data.count("\t\tshutdownChildren(children)\n") != 1:
    raise SystemExit("unexpected cleanup source; no mutation made")
with path.open("w", encoding="utf-8", newline="\n") as output:
    output.write(data.replace(new, old))
print("restored_old_output_close_before_stop_order")
