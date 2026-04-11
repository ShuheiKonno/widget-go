@echo off
setlocal
set OUT=%~dp0_findgo_result.txt
echo start > "%OUT%"
if exist "C:\Program Files\Go\bin\go.exe" echo FOUND_PF >> "%OUT%"
if exist "C:\Program Files (x86)\Go\bin\go.exe" echo FOUND_PF86 >> "%OUT%"
if exist "C:\Go\bin\go.exe" echo FOUND_CGO >> "%OUT%"
if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" echo FOUND_LOCAL >> "%OUT%"
if exist "%USERPROFILE%\scoop\apps\go\current\bin\go.exe" echo FOUND_SCOOP >> "%OUT%"
if exist "C:\ProgramData\chocolatey\bin\go.exe" echo FOUND_CHOCO >> "%OUT%"
echo done >> "%OUT%"
endlocal
