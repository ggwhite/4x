!macro NSIS_HOOK_PREINSTALL
  nsExec::Exec 'taskkill /F /IM fourx-live.exe'
  nsExec::Exec 'taskkill /F /IM 4x.exe'
  Sleep 500
!macroend
