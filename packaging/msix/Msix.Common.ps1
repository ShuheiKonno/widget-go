# Shared packaging helpers. No signing, certificate trust, or installation.
function Resolve-MsixSdkBin {
    param([string]$SdkBin)
    if ($SdkBin) {
        if (-not (Test-Path -LiteralPath (Join-Path $SdkBin 'makeappx.exe'))) {
            throw 'The specified SDK directory does not contain MakeAppx.exe.'
        }
        return $SdkBin
    }
    $kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $sdk = Get-ChildItem $kits -Directory | Where-Object {
        $_.Name -match '^10\.0\.\d+\.0$' -and
        (Test-Path (Join-Path $_.FullName 'x64\makeappx.exe')) -and
        (Test-Path (Join-Path $_.FullName 'x64\signtool.exe'))
    } | Sort-Object { [version]$_.Name } -Descending | Select-Object -First 1
    if (-not $sdk) { throw 'Install the Windows SDK with MakeAppx and SignTool first.' }
    return (Join-Path $sdk.FullName 'x64')
}

function New-MsixLogoAssets {
    param([string]$IconPath, [string]$Destination, [string]$StoreIconPath)
    Add-Type -AssemblyName System.Drawing
    # Extract the largest PNG frame: Icon.ToBitmap fails with this PNG-backed ICO
    # on Windows PowerShell. The original project artwork stays unchanged.
    $icoBytes = [IO.File]::ReadAllBytes($IconPath)
    $frameCount = [BitConverter]::ToUInt16($icoBytes, 4)
    $imageOffset, $imageLength, $largest = 0, 0, 0
    for ($i = 0; $i -lt $frameCount; $i++) {
        $entryOffset = 6 + 16 * $i
        $size = [int]$icoBytes[$entryOffset]
        if ($size -eq 0) { $size = 256 }
        if ($size -gt $largest) {
            $largest = $size
            $imageLength = [BitConverter]::ToUInt32($icoBytes, $entryOffset + 8)
            $imageOffset = [BitConverter]::ToUInt32($icoBytes, $entryOffset + 12)
        }
    }
    if ($imageLength -lt 8 -or $imageOffset + $imageLength -gt $icoBytes.Length -or
        [BitConverter]::ToString($icoBytes, $imageOffset, 8) -ne '89-50-4E-47-0D-0A-1A-0A') {
        throw 'Expected a valid PNG frame in the project icon.ico.'
    }
    $sourceStream = [IO.MemoryStream]::new($icoBytes, $imageOffset, $imageLength)
    $source = [Drawing.Image]::FromStream($sourceStream)
    try {
        $entries = @(
            @{ Path = (Join-Path $Destination 'Square44x44Logo.png'); Size = 44 },
            @{ Path = (Join-Path $Destination 'Square150x150Logo.png'); Size = 150 },
            @{ Path = (Join-Path $Destination 'StoreLogo.png'); Size = 50 }
        )
        if ($StoreIconPath) { $entries += @{ Path = $StoreIconPath; Size = 300 } }
        foreach ($entry in $entries) {
            $bitmap = New-Object System.Drawing.Bitmap($entry.Size, $entry.Size)
            $graphics = [Drawing.Graphics]::FromImage($bitmap)
            try {
                $graphics.Clear([Drawing.Color]::Transparent)
                $graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $graphics.DrawImage($source, 0, 0, $entry.Size, $entry.Size)
                $bitmap.Save($entry.Path, [Drawing.Imaging.ImageFormat]::Png)
            } finally {
                $graphics.Dispose()
                $bitmap.Dispose()
            }
        }
    } finally {
        $source.Dispose()
        $sourceStream.Dispose()
    }
}
