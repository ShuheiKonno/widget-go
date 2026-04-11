$result = Get-Command go -ErrorAction SilentlyContinue
if ($result) {
    $result.Source | Out-File -Encoding ascii -FilePath "$PSScriptRoot\_findgo_result.txt"
} else {
    "NOT_FOUND" | Out-File -Encoding ascii -FilePath "$PSScriptRoot\_findgo_result.txt"
}
