#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$IdentityFile,
    [ValidatePattern('^\d+\.\d+\.\d+\.\d+$')]
    [string]$Version,
    [string]$SdkBin,
    [string]$OutputRoot,
    [switch]$AllowDirty
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Msix.Common.ps1')
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if (-not $IdentityFile) { $IdentityFile = Join-Path $PSScriptRoot 'store-identity.json' }
$identity = Get-Content -LiteralPath $IdentityFile -Raw -Encoding UTF8 | ConvertFrom-Json
if ($identity.identityName -notmatch '^[A-Za-z0-9.-]{3,50}$' -or
    $identity.publisher -notmatch '^CN=[0-9A-Fa-f-]{36}$' -or
    [string]::IsNullOrWhiteSpace($identity.displayName) -or
    [string]::IsNullOrWhiteSpace($identity.publisherDisplayName)) {
    throw 'Use the exact product identity from Partner Center; the identity file is incomplete.'
}
$resources = Get-Content (Join-Path $repo 'winres\winres.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$resourceVersion = $resources.RT_VERSION.'#1'.'0000'.fixed.file_version
if (-not $Version) { $Version = $resourceVersion }
$parts = $Version.Split('.')
if ($parts.Count -ne 4 -or [int]$parts[0] -lt 1 -or @($parts | Where-Object { [int]$_ -gt 65535 }).Count) {
    throw 'Use a four-part MSIX version with components <= 65535 and major >= 1.'
}
if ($parts[3] -ne '0') { throw 'Store package versions must have a final component of 0.' }
if ($Version -ne $resourceVersion) { throw 'Store version must match winres file_version.' }
$SdkBin = Resolve-MsixSdkBin $SdkBin
Push-Location $repo
try {
    $sourceCommit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the source commit.' }
    $dirty = [bool](& git status --porcelain)
    if ($dirty -and -not $AllowDirty) { throw 'Commit and review changes before building a Store submission. Use -AllowDirty for local drafts only.' }
} finally { Pop-Location }

if (-not $OutputRoot) { $OutputRoot = Join-Path $repo 'dist\msix-store' }
$buildId = '{0}-{1}-{2}' -f $Version, (Get-Date -Format 'yyyyMMdd-HHmmss'), ([guid]::NewGuid().ToString('N').Substring(0, 8))
$output = [IO.Path]::GetFullPath((Join-Path $OutputRoot $buildId))
$stage = Join-Path $output 'package'
$assets = Join-Path $stage 'Assets'
$listing = Join-Path $output 'store-listing'
New-Item -ItemType Directory -Path $assets, $listing -Force | Out-Null
New-MsixLogoAssets -IconPath (Join-Path $repo 'icon.ico') -Destination $assets -StoreIconPath (Join-Path $listing 'app-icon-300.png')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'store-listing.ja-jp.json') -Destination $listing
Copy-Item -LiteralPath (Join-Path $repo 'packaging\store-assets\screenshots') -Destination $listing -Recurse

$manifest = New-Object Xml.XmlDocument
$manifest.Load((Join-Path $PSScriptRoot 'StoreManifest.xml'))
$manifest.Package.Identity.SetAttribute('Name', $identity.identityName)
$manifest.Package.Identity.SetAttribute('Publisher', $identity.publisher)
$manifest.Package.Identity.SetAttribute('Version', $Version)
$manifest.Package.Properties.DisplayName = [string]$identity.displayName
$manifest.Package.Properties.PublisherDisplayName = [string]$identity.publisherDisplayName
$application = $manifest.Package.Applications.Application
$application.SetAttribute('Id', $identity.applicationId)
$application.VisualElements.SetAttribute('DisplayName', $identity.displayName)
$application.Extensions.Extension.StartupTask.SetAttribute('DisplayName', $identity.displayName)
$manifest.Save((Join-Path $stage 'AppxManifest.xml'))
foreach ($name in @('LICENSE', 'THIRD_PARTY_NOTICES.md', 'PRIVACY.md')) {
    Copy-Item -LiteralPath (Join-Path $repo $name) -Destination $stage
}

$ldflags = '-H windowsgui -s -w -X main.appDataDirectoryName=widget-go-store -X main.singleInstanceMutexName=Local\widget-go-store-single-instance-mutex -X main.applicationWindowTitle=SystemWidget -X main.distributionChannel=store'
$oldGoos, $oldGoarch, $oldCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $repo
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & go build -trimpath -ldflags $ldflags -o (Join-Path $stage 'system-widget.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
} finally {
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
    $env:CGO_ENABLED = $oldCgo
    Pop-Location
}
$fileName = "SystemWidget-Store_${Version}_x64.msix"
$package = Join-Path $output $fileName
& (Join-Path $SdkBin 'makeappx.exe') pack /d $stage /p $package /h SHA256
if ($LASTEXITCODE -ne 0) { throw 'MakeAppx validation or packaging failed.' }

# Store MSIX submissions do not need a developer signing certificate.
# Microsoft signs the package after certification. Never use the test certificate.
$info = [ordered]@{
    packageName = $identity.identityName
    publisher = $identity.publisher
    displayName = $identity.displayName
    storeId = $identity.storeId
    version = $Version
    architecture = 'x64'
    packageFile = $fileName
    packageSha256 = (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash
    sourceCommit = $sourceCommit
    workingTreeDirty = $dirty
    submissionReady = -not $dirty
    createdUtc = [DateTime]::UtcNow.ToString('o')
    signing = 'Unsigned MSIX for Microsoft Store submission; Microsoft signs after certification.'
}
$info | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'package-info.json') -Encoding UTF8
"$($info.packageSha256.ToLowerInvariant())  $fileName" | Set-Content -LiteralPath "$package.sha256" -Encoding ASCII
Write-Output "Store submission package: $package"
Write-Output "Store ID: $($identity.storeId)"
Write-Output "Clean source: $(-not $dirty)"
