﻿$ErrorActionPreference = 'Stop';

$packageName= 'glouton'
$toolsDir   = "$(Split-Path -parent $MyInvocation.MyCommand.Definition)"
$fileLocation = Join-Path $toolsDir 'glouton.msi'

$packageArgs = @{
  packageName   = $packageName
  fileType      = 'msi'
  file         = $fileLocation
  softwareName  = 'Glouton*'
  silentArgs    = "/qn /norestart /l*v `"$($env:TEMP)\$($packageName).$($env:chocolateyPackageVersion).MsiInstall.log`""
  validExitCodes= @(0, 3010, 1641)
}

Install-ChocolateyInstallPackage @packageArgs
