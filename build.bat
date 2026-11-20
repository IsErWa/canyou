@echo off
call "%~dp0scripts\build.bat" %*
exit /b %errorlevel%
