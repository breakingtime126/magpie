package nativepreview

import (
 "path/filepath"

 "golang.org/x/sys/windows"
)

// Credential-bearing native state must not silently land on a network share,
// mapped remote drive, or a reparse-point ancestor. This is a pre-use check,
// not an ACL change or a race-free filesystem security boundary.
func safeStateParent(path string) bool {
 if !filepath.IsAbs(path){return false}
 path=filepath.Clean(path)
 volume:=filepath.VolumeName(path)
 if len(volume)!=2 || volume[1]!=':' || !((volume[0]>='A' && volume[0]<='Z') || (volume[0]>='a' && volume[0]<='z')){return false}
 root:=volume+`\`
 rootUTF16,err:=windows.UTF16PtrFromString(root)
 if err!=nil || windows.GetDriveType(rootUTF16)!=windows.DRIVE_FIXED{return false}
 for {
  current,err:=windows.UTF16PtrFromString(path)
  if err!=nil{return false}
  attrs,err:=windows.GetFileAttributes(current)
  if err!=nil || attrs&windows.FILE_ATTRIBUTE_DIRECTORY==0 || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT!=0{return false}
  parent:=filepath.Dir(path)
  if parent==path{return true}
  path=parent
 }
}
