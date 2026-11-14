#requires -Version 5.1
[CmdletBinding(SupportsShouldProcess = $true)]
param([switch]$TrustTestCertificate)

$ErrorActionPreference = 'Stop'
$info = Get-Content (Join-Path $PSScriptRoot 'package-info.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if ($info.packageName -ne 'ShuheiKonno.SystemWidget.Test' -or $info.publisher -ne 'CN=widget-go MSIX Test') {
    throw 'Unexpected package identity. This installer accepts the local test package only.'
}
foreach ($name in @($info.packageFile, $info.certificateFile)) {
    if ([string]::IsNullOrWhiteSpace($name) -or [IO.Path]::GetFileName($name) -ne $name -or
        $name -in @('.', '..')) { throw 'Unsafe file name in package-info.json.' }
}
$package = Join-Path $PSScriptRoot $info.packageFile
$cer = Join-Path $PSScriptRoot $info.certificateFile
$hashStream = [IO.File]::OpenRead($package)
$sha256 = [Security.Cryptography.SHA256]::Create()
try {
    $packageHash = [BitConverter]::ToString($sha256.ComputeHash($hashStream)).Replace('-', '')
} finally {
    $sha256.Dispose()
    $hashStream.Dispose()
}
if ($packageHash -ne $info.packageSha256) {
    throw 'MSIX SHA-256 does not match package-info.json.'
}
$certificate = New-Object Security.Cryptography.X509Certificates.X509Certificate2($cer)
$signature = Get-AuthenticodeSignature -LiteralPath $package
if ($certificate.Subject -ne $info.publisher -or $certificate.Thumbprint -ne $info.certificateThumbprint -or
    $certificate.HasPrivateKey -or $signature.SignerCertificate.Thumbprint -ne $certificate.Thumbprint) {
    throw 'Certificate or package signer mismatch.'
}
if ($certificate.NotAfter -le (Get-Date) -or $certificate.NotBefore -gt (Get-Date)) {
    throw 'The test certificate is expired or is not valid yet. Rebuild the package.'
}
Write-Output "Package: $($info.packageName) $($info.version)"
Write-Output "Test certificate: $($certificate.Subject)"
Write-Output "Thumbprint: $($certificate.Thumbprint)"

$trustPath = "Cert:\LocalMachine\TrustedPeople\$($certificate.Thumbprint)"
if (-not (Test-Path -LiteralPath $trustPath)) {
    if (-not $TrustTestCertificate) {
        throw 'Test certificate is not trusted. Review its thumbprint, then run this script in an administrator PowerShell with -TrustTestCertificate. No trust was added.'
    }
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) -and -not $WhatIfPreference) {
        throw 'Adding the development certificate to LocalMachine\TrustedPeople requires an administrator PowerShell.'
    }
    if ($PSCmdlet.ShouldProcess($trustPath, 'Trust this development signing certificate (TrustedPeople, not Root)')) {
        Import-Certificate -FilePath $cer -CertStoreLocation 'Cert:\LocalMachine\TrustedPeople' | Out-Null
    }
}
if ($PSCmdlet.ShouldProcess($package, 'Install the local MSIX test app for the current Windows user')) {
    Add-AppxPackage -Path $package
    $installed = Get-AppxPackage -Name $info.packageName
    if (-not $installed -or $installed.Version -ne $info.version -or $installed.Publisher -ne $info.publisher) {
        throw 'Installed package verification failed.'
    }
    Write-Output "Installed: $($installed.PackageFullName)"
    Write-Output "Launch: explorer.exe shell:AppsFolder\$($installed.PackageFamilyName)!$($info.applicationId)"
}
