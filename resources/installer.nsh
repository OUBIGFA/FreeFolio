; Interactive installs keep MyGo's finish page. Only an in-app update asks
; the silent installer to launch FreeFolio after all files have been replaced.
!include "FileFunc.nsh"

!macro customInstall
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/FREEFOLIOUPDATE" $1
  IfErrors freefolio_update_done
  Exec '"$INSTDIR\FreeFolio.exe"'
  freefolio_update_done:
!macroend
