#!/usr/bin/env python3
"""Restore only old shutdown sequencing in a disposable source archive."""
import sys
from pathlib import Path

if len(sys.argv) != 2:
    raise SystemExit("expected the disposable archive's run.go path")
path = Path(sys.argv[1])
data = path.read_text(encoding="utf-8")
new = """func shutdownChildren(children map[string]*childServer) error {
	for _, child := range children {
		child.input.Close()
	}
	errorsCh := make(chan error, len(children))
	ownedCount := 0
	for _, child := range children {
		if child.shutdown != nil {
			ownedCount++
			go func(c *childServer) { errorsCh <- c.shutdown() }(child)
		}
	}
	stopChildren(children)
	var result error
	for range ownedCount {
		result = errors.Join(result, <-errorsCh)
	}
	for _, child := range children {
		if child.shutdown == nil && child.streams == nil {
			child.output.Close()
		} // Owned streams close only after real I/O ends.
	}
	return result
}"""
close_outputs = """	for _, child := range children {
		if child.shutdown == nil && child.streams == nil {
			child.output.Close()
		} // Owned streams close only after real I/O ends.
	}
"""
caller = """		if err := shutdownChildren(children); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
"""
# Move only ordinary child output closure ahead of tree stopping. Keep owned
# endpoint shutdown, owned-stream guards, error collection and caller handling
# intact, so the mutation tests sequencing rather than a different cleanup bug.
old = new.replace(close_outputs, "").replace(
    "\tstopChildren(children)\n", close_outputs + "\tstopChildren(children)\n"
)
if data.count(new) != 1 or data.count(caller) != 1:
    raise SystemExit("unexpected cleanup source; no mutation made")
with path.open("w", encoding="utf-8", newline="\n") as output:
    output.write(data.replace(new, old))
print("restored_old_output_close_before_stop_order")
