; Interactive installs keep MyGo's finish page. Only an in-app update asks
; the silent installer to launch FreeMD after all files have been replaced.
!include "FileFunc.nsh"

!macro customInstall
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/FREEMDUPDATE" $1
  IfErrors freemd_update_done
  Exec '"$INSTDIR\FreeMD.exe"'
  freemd_update_done:
!macroend
