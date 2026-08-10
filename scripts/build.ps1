param([string]$Version = "dev")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force dist | Out-Null
$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe" },
    @{ GOOS = "linux";   GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "darwin";  GOARCH = "arm64"; Ext = "" }
)
foreach ($t in $targets) {
    $out = "dist/crema-$($t.GOOS)-$($t.GOARCH)$($t.Ext)"
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w -X main.Version=$Version" -o $out ./cmd/crema
    if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.GOOS)/$($t.GOARCH)" }
    $mb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    "{0,-36} {1} MB" -f $out, $mb
    if ($mb -gt 15) { throw "$out is $mb MB — over the 15 MB budget" }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
"all targets built and under budget"
