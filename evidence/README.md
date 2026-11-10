# Staged acceptance sources

This directory is outside the product Go module. Its overlay directories are
incomplete source snapshots, not independent packages. Their runners verify the
listed baseline and source hashes before copying the listed files into an
isolated module and running the specified checks.

The module boundary prevents the product's `go vet ./...` and `go test ./...`
from compiling those staged copies against unrelated production imports. It
does not disable the dedicated overlay workflows, change their manifests, or
promote staged behavior into the application. The root buildvariant regression
checks the actual Windows package enumeration with all network access disabled
in the Go command.
