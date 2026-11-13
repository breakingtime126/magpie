// Runtime path diagnostic only: no file content reads, writes, hashing,
// network clients, child processes, credentials, or configuration access.
package main

import (
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "syscall"
)

type result struct {
 Schema int `json:"schema"`
 GoVersion string `json:"goVersion"`
 Stage string `json:"stage"`
 Outcome string `json:"outcome"`
 ErrorKind string `json:"errorKind,omitempty"`
 ErrorCode uint64 `json:"errorCode,omitempty"`
 Operation string `json:"operation,omitempty"`
 Regular bool `json:"regular"`
 Size int64 `json:"sizeBytes"`
}
func failure(r result, stage string, err error) result {
 r.Stage, r.Outcome = stage, "failed"
 r.ErrorKind = "other"
 if errors.Is(err, os.ErrNotExist) { r.ErrorKind = "not-exist" }
 if errors.Is(err, os.ErrPermission) { r.ErrorKind = "permission" }
 var pe *os.PathError
 if errors.As(err, &pe) { r.Operation = pe.Op }
 var code syscall.Errno
 if errors.As(err, &code) { r.ErrorCode = uint64(code) }
 return r
}
func inspect(path string) result {
 r := result{Schema:1, GoVersion:runtime.Version(), Stage:"input", Outcome:"failed"}
 if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), "codex.exe") { return r }
 resolved, err := filepath.EvalSymlinks(path)
 if err != nil { return failure(r, "runtime-path-resolution", err) }
 if !strings.EqualFold(filepath.Clean(path), filepath.Clean(resolved)) {
  r.Stage = "runtime-path-identity"
  return r
 }
 info, err := os.Lstat(resolved)
 if err != nil { return failure(r, "runtime-file-info", err) }
 r.Stage, r.Regular, r.Size = "runtime-file-info", info.Mode().IsRegular(), info.Size()
 if r.Regular { r.Outcome = "completed" }
 return r
}
func run(args []string, out io.Writer) int {
 if len(args)!=1 {
  fmt.Fprintln(out, "usage: runtime-path-probe.exe <absolute path to codex.exe>")
  return 2
 }
 r := inspect(args[0])
 if json.NewEncoder(out).Encode(r)!=nil { return 2 }
 if r.Outcome!="completed" { return 1 }
 return 0
}
func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
