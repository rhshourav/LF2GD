$ErrorActionPreference = "Stop"

$targets = @(
    @{GOOS="windows"; GOARCH="amd64"; OUT="build/WillcomDownloader_windows_amd64.exe"},
    @{GOOS="windows"; GOARCH="386";   OUT="build/WillcomDownloader_windows_386.exe"},
    @{GOOS="windows"; GOARCH="arm64"; OUT="build/WillcomDownloader_windows_arm64.exe"},
    @{GOOS="linux";   GOARCH="amd64"; OUT="build/WillcomDownloader_linux_amd64"},
    @{GOOS="linux";   GOARCH="arm64"; OUT="build/WillcomDownloader_linux_arm64"},
    @{GOOS="darwin";  GOARCH="amd64"; OUT="build/WillcomDownloader_macos_amd64"},
    @{GOOS="darwin";  GOARCH="arm64"; OUT="build/WillcomDownloader_macos_arm64"}
)

New-Item -ItemType Directory -Force -Path build | Out-Null

foreach ($t in $targets) {
    Write-Host "Building $($t.GOOS)/$($t.GOARCH)..."

    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH

    go build -ldflags="-s -w" -o $t.OUT main.go
}

Write-Host "Build completed."