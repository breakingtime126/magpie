package nativepreview

import (
 "unsafe"

 "golang.org/x/sys/windows"
)

// Do not request breakaway or change an inherited job. Its lifetime could
// otherwise affect a newly launched user browser even with root-only cleanup.
func nativeLoginAllowed() bool {
 var inJob uint32
 call:=windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
 if call.Find()!=nil{return false}
 ok,_,_:=call.Call(uintptr(windows.CurrentProcess()),0,uintptr(unsafe.Pointer(&inJob)))
 return ok!=0 && inJob==0
}
