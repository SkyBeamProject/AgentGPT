# Build a sideloadable MSIX on Windows. makeappx and signtool ship with the Windows SDK.
# Unsigned packages cannot be installed, so this script creates a certificate for the current user.
param(
    [string]$Exe = "",
    [string]$OutFile = "BeamAgent.msix"
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $Exe) {
    $Exe = Join-Path $here "beam-agent.exe"
}
if (-not (Test-Path $Exe)) {
    throw "Copy beam-agent.exe next to this script, or pass -Exe."
}

$sdk = Get-ChildItem "C:\Program Files (x86)\Windows Kits\10\bin" -ErrorAction SilentlyContinue |
    Sort-Object Name |
    Select-Object -Last 1
if (-not $sdk) {
    throw "Windows SDK not found. Install it, then rerun this script. The .exe setup and .msi do not need the SDK."
}
$makeappx = Join-Path $sdk.FullName "x64\makeappx.exe"
$signtool = Join-Path $sdk.FullName "x64\signtool.exe"

$layout = Join-Path $env:TEMP "beam-agent-msix"
if (Test-Path $layout) { Remove-Item $layout -Recurse -Force }
New-Item -ItemType Directory -Path $layout | Out-Null
Copy-Item $Exe (Join-Path $layout "beam-agent.exe")
Copy-Item (Join-Path $here "AppxManifest.xml") (Join-Path $layout "AppxManifest.xml")
Copy-Item (Join-Path $here "assets") (Join-Path $layout "assets") -Recurse
foreach ($extra in @("LICENSE.txt", "SOURCE.txt")) {
    $path = Join-Path $here $extra
    if (Test-Path $path) { Copy-Item $path (Join-Path $layout $extra) }
}

$out = Join-Path (Get-Location) $OutFile
if (Test-Path $out) { Remove-Item $out -Force }
& $makeappx pack /d $layout /p $out /o
if ($LASTEXITCODE -ne 0) { throw "makeappx failed" }

$subject = "CN=SkyBeam Beam Agent"
$cert = Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Subject -eq $subject } | Select-Object -First 1
if (-not $cert) {
    $cert = New-SelfSignedCertificate -Type Custom -Subject $subject -KeyUsage DigitalSignature `
        -FriendlyName "Beam Agent" -CertStoreLocation Cert:\CurrentUser\My `
        -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3")
}
$cer = Join-Path $env:TEMP "beam-agent.cer"
Export-Certificate -Cert $cert -FilePath $cer | Out-Null
Import-Certificate -FilePath $cer -CertStoreLocation Cert:\CurrentUser\TrustedPeople | Out-Null
& $signtool sign /fd SHA256 /sha1 $cert.Thumbprint $out
if ($LASTEXITCODE -ne 0) { throw "signtool failed" }

Add-AppxPackage -Path $out
Write-Host "Installed $out"
