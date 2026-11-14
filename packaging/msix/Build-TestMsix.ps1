#requires -Version 5.1
[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+\.\d+$')]
    [string]$Version,
    [string]$SdkBin,
    [string]$OutputRoot,
    [ValidatePattern('^[0-9A-Fa-f]{40}$')]
    [string]$CertificateThumbprint
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Msix.Common.ps1')
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$subject = 'CN=widget-go MSIX Test'
$friendlyName = 'widget-go local MSIX test (not for production)'

if (-not $Version) {
    $resources = Get-Content (Join-Path $repo 'winres\winres.json') -Raw -Encoding UTF8 | ConvertFrom-Json
    $Version = $resources.RT_VERSION.'#1'.'0000'.fixed.file_version
}
$parts = $Version.Split('.')
if ($parts.Count -ne 4 -or [int]$parts[0] -lt 1 -or @($parts | Where-Object { [int]$_ -gt 65535 }).Count) {
    throw 'Use a four-part MSIX version: major >= 1 and each component <= 65535.'
}
$SdkBin = Resolve-MsixSdkBin $SdkBin
$makeappx = Join-Path $SdkBin 'makeappx.exe'
$signtool = Join-Path $SdkBin 'signtool.exe'
foreach ($tool in @($makeappx, $signtool)) {
    if (-not (Test-Path -LiteralPath $tool -PathType Leaf)) { throw "Missing SDK tool: $tool" }
}
if (-not $OutputRoot) { $OutputRoot = Join-Path $repo 'dist\msix-test' }
$buildId = '{0}-{1}-{2}' -f $Version, (Get-Date -Format 'yyyyMMdd-HHmmss'), ([guid]::NewGuid().ToString('N').Substring(0, 8))
$output = [IO.Path]::GetFullPath((Join-Path $OutputRoot $buildId))
$stage = Join-Path $output 'package'
$assets = Join-Path $stage 'Assets'
New-Item -ItemType Directory -Path $assets -Force | Out-Null

$manifest = New-Object System.Xml.XmlDocument
$manifest.Load((Join-Path $PSScriptRoot 'AppxManifest.xml'))
$manifest.Package.Identity.SetAttribute('Version', $Version)
$manifest.Save((Join-Path $stage 'AppxManifest.xml'))

New-MsixLogoAssets -IconPath (Join-Path $repo 'icon.ico') -Destination $assets

# A fixed allowlist keeps local settings, browser profiles, logs and secrets out.
foreach ($name in @('LICENSE', 'THIRD_PARTY_NOTICES.md', 'PRIVACY.md')) {
    Copy-Item -LiteralPath (Join-Path $repo $name) -Destination $stage
}
$ldflags = '-H windowsgui -s -w -X main.appDataDirectoryName=widget-go-msix-test/data -X main.singleInstanceMutexName=Local\widget-go-msix-test-single-instance-mutex -X main.applicationWindowTitle=SystemWidget-MSIX-Test -X widget-go/shortcut.FileName=system-widget-msix-test.lnk'
$oldGoos, $oldGoarch, $oldCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $repo
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & go build -trimpath -ldflags $ldflags -o (Join-Path $stage 'system-widget.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    $sourceCommit = (& git rev-parse HEAD).Trim()
    $dirty = [bool](& git status --porcelain)
} finally {
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
    $env:CGO_ENABLED = $oldCgo
    Pop-Location
}

$fileName = "SystemWidget-Test_${Version}_x64.msix"
$package = Join-Path $output $fileName
# Keep MakeAppx validation enabled (no /nv). Never overwrite an earlier build.
& $makeappx pack /d $stage /p $package /h SHA256
if ($LASTEXITCODE -ne 0) { throw 'MakeAppx validation or packaging failed.' }

# A non-exportable development key remains in the current user's Personal store.
# Creating it does NOT add trust, install the app, or change the machine policy.
if ($CertificateThumbprint) {
    $certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$CertificateThumbprint"
} else {
    $certificate = Get-ChildItem Cert:\CurrentUser\My | Where-Object {
        $_.Subject -eq $subject -and $_.FriendlyName -eq $friendlyName -and
        $_.HasPrivateKey -and $_.NotAfter -gt (Get-Date).AddDays(2)
    } | Sort-Object NotAfter -Descending | Select-Object -First 1
    if (-not $certificate) {
        $certificate = New-SelfSignedCertificate -Type Custom -Subject $subject `
            -FriendlyName $friendlyName -CertStoreLocation 'Cert:\CurrentUser\My' `
            -KeyAlgorithm RSA -KeyLength 2048 -HashAlgorithm SHA256 `
            -KeyUsage DigitalSignature -KeyExportPolicy NonExportable `
            -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3', '2.5.29.19={text}') `
            -NotAfter (Get-Date).AddDays(30)
    }
}
if ($certificate.Subject -ne $subject -or -not $certificate.HasPrivateKey -or
    $certificate.NotAfter -le (Get-Date) -or $certificate.NotBefore -gt (Get-Date)) {
    throw 'The certificate must be a valid widget-go MSIX development certificate with a private key.'
}
& $signtool sign /fd SHA256 /s My /sha1 $certificate.Thumbprint $package
if ($LASTEXITCODE -ne 0) { throw 'SignTool signing failed.' }
$cerName = 'widget-go-msix-test.cer'
Export-Certificate -Cert $certificate -FilePath (Join-Path $output $cerName) | Out-Null

$info = [ordered]@{
    packageName = 'ShuheiKonno.SystemWidget.Test'
    applicationId = 'Widget'
    publisher = $subject
    version = $Version
    architecture = 'x64'
    packageFile = $fileName
    packageSha256 = (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash
    certificateFile = $cerName
    certificateThumbprint = $certificate.Thumbprint
    certificateExpires = $certificate.NotAfter.ToUniversalTime().ToString('o')
    sourceCommit = $sourceCommit
    workingTreeDirty = $dirty
    createdUtc = [DateTime]::UtcNow.ToString('o')
    purpose = 'Local compatibility testing only. Not Microsoft Store or public production signing.'
}
$info | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'package-info.json') -Encoding UTF8
"$($info.packageSha256.ToLowerInvariant())  $fileName" | Set-Content -LiteralPath "$package.sha256" -Encoding ASCII
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Install-TestMsix.ps1') -Destination $output
Copy-Item -LiteralPath (Join-Path $repo 'docs\msix-test.md') -Destination (Join-Path $output 'README-msix-test.md')
$kit = Join-Path $output "SystemWidget-Test_${Version}_x64-test-kit.zip"
$kitFiles = @($package, "$package.sha256", (Join-Path $output $cerName),
    (Join-Path $output 'package-info.json'), (Join-Path $output 'Install-TestMsix.ps1'),
    (Join-Path $output 'README-msix-test.md'))
Compress-Archive -LiteralPath $kitFiles -DestinationPath $kit -CompressionLevel Optimal
Write-Output "MSIX test package: $package"
Write-Output "Test kit (package, public certificate, installer and instructions): $kit"
Write-Output "Development certificate thumbprint: $($certificate.Thumbprint)"
Write-Output 'Certificate trust and app installation have NOT been performed by this build script.'
