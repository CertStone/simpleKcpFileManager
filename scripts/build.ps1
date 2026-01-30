param(
    [string]$OutDir = "dist",
    [switch]$AllowCgoCross
)

$ErrorActionPreference = "Stop"

$hostOS = (go env GOHOSTOS)
$hostArch = (go env GOHOSTARCH)

$matrix = @(
    @{ GOOS = "windows"; GOARCH = "amd64" }
    @{ GOOS = "windows"; GOARCH = "arm64" }
    @{ GOOS = "linux";   GOARCH = "amd64" }
    @{ GOOS = "linux";   GOARCH = "arm64" }
)

$packages = @("./server", "./client")

if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }

foreach ($item in $matrix) {
    $env:GOOS = $item.GOOS
    $env:GOARCH = $item.GOARCH

    foreach ($pkg in $packages) {
        $name = Split-Path $pkg -Leaf
        $ext = $(if ($env:GOOS -eq "windows") { ".exe" } else { "" })
        $out = Join-Path $OutDir "$name-$($env:GOOS)-$($env:GOARCH)$ext"

        # Server has no cgo requirements; disable to allow cross builds without toolchains.
        if ($name -eq "server") {
            $env:CGO_ENABLED = "0"
        } else {
            $env:CGO_ENABLED = "1"
            # Skip GUI cross-builds when host OS differs unless explicitly allowed.
            if (-not $AllowCgoCross -and $env:GOOS -ne $hostOS) {
                Write-Warning "Skipping $name for $($env:GOOS)/$($env:GOARCH); cross CGO build requires target toolchain. Use -AllowCgoCross after installing one."
                continue
            }
        }

        Write-Host "[build] $out"
        go build -o $out $pkg
    }
}

Write-Host "Artifacts are in $OutDir" 