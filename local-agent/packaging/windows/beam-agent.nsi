!include "MUI2.nsh"
!include "LogicLib.nsh"

!ifndef VERSION
  !define VERSION "0.1.0"
!endif

Name "Beam Agent"
OutFile "BeamAgentSetup.exe"
Unicode True
InstallDir "$LOCALAPPDATA\Programs\Beam Agent"
RequestExecutionLevel user
BrandingText "Beam Agent ${VERSION}"
ShowInstDetails show

!define MUI_ICON "beam-agent.ico"
!define MUI_UNICON "beam-agent.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\beam-agent.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start Beam Agent"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "LICENSE.txt"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetShellVarContext current
  SetOutPath $INSTDIR
  File "beam-agent.exe"
  File "LICENSE.txt"
  File "SOURCE.txt"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  CreateDirectory "$SMPROGRAMS\Beam Agent"
  CreateShortcut "$SMPROGRAMS\Beam Agent\Beam Agent.lnk" "$INSTDIR\beam-agent.exe" "" "$INSTDIR\beam-agent.exe" 0
  CreateShortcut "$SMPROGRAMS\Beam Agent\Uninstall Beam Agent.lnk" "$INSTDIR\Uninstall.exe"

  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "DisplayName" "Beam Agent"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "Publisher" "SkyBeam Project"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  Delete "$SMPROGRAMS\Beam Agent\Beam Agent.lnk"
  Delete "$SMPROGRAMS\Beam Agent\Uninstall Beam Agent.lnk"
  RMDir "$SMPROGRAMS\Beam Agent"
  Delete "$INSTDIR\beam-agent.exe"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\SOURCE.txt"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeamAgent"
SectionEnd
