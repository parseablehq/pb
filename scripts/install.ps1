$ErrorActionPreference = "Stop"

$Repo = "parseablehq/pb"
$BinaryName = "pb"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:USERPROFILE "bin" }
$Version = if ($env:VERSION) { $env:VERSION } else { "latest" }

function Get-Architecture {
    switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
        "x64" { "amd64" }
        "arm64" { "arm64" }
        default { throw "Unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
    }
}

if ($Version -eq "latest") {
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $Release.tag_name
}
else {
    $Version = $Version.TrimStart("v")
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/tags/v$Version"
}

$Version = $Version.TrimStart("v")
$Arch = Get-Architecture
$AssetPrefix = "${BinaryName}_${Version}_windows_${Arch}"
$Asset = $Release.assets |
    Where-Object { $_.name -eq "$AssetPrefix.zip" -or $_.name -eq "$AssetPrefix.tar.gz" } |
    Select-Object -First 1

if (-not $Asset) {
    throw "No Windows $Arch asset found for v$Version"
}

$Checksums = "${BinaryName}_${Version}_checksums.txt"
$BaseUrl = "https://github.com/$Repo/releases/download/v$Version"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())

New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

try {
    $AssetPath = Join-Path $TempDir $Asset.name
    $ChecksumsPath = Join-Path $TempDir $Checksums

    Write-Host "Downloading $($Asset.name)..."
    Invoke-WebRequest -Uri $Asset.browser_download_url -OutFile $AssetPath
    Invoke-WebRequest -Uri "$BaseUrl/$Checksums" -OutFile $ChecksumsPath

    $Expected = Select-String -Path $ChecksumsPath -Pattern "\s$([regex]::Escape($Asset.name))$" |
        ForEach-Object { ($_.Line -split "\s+")[0] } |
        Select-Object -First 1

    if (-not $Expected) {
        throw "Checksum for $($Asset.name) not found in $Checksums"
    }

    $Actual = (Get-FileHash -Algorithm SHA256 -Path $AssetPath).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected.ToLowerInvariant()) {
        throw "Checksum verification failed for $($Asset.name)"
    }

    if ($Asset.name.EndsWith(".zip")) {
        Expand-Archive -Path $AssetPath -DestinationPath $TempDir -Force
    }
    else {
        tar -xzf $AssetPath -C $TempDir
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Move-Item -Force -Path (Join-Path $TempDir "$BinaryName.exe") -Destination (Join-Path $InstallDir "$BinaryName.exe")

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $PathEntries = $UserPath -split ";" | Where-Object { $_ }
    if ($PathEntries -notcontains $InstallDir) {
        $NewUserPath = if ($UserPath) { "$UserPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable("Path", $NewUserPath, "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "Added $InstallDir to your user PATH. Open a new PowerShell window to use $BinaryName from any directory."
    }

    Write-Host "$BinaryName installed to $(Join-Path $InstallDir "$BinaryName.exe")"
}
finally {
    Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
}
