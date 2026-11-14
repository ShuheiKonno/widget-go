package main

// 通常版の既定値。MSIX テスト版・Store版は -ldflags -X で識別子を分ける。
// 環境変数や実行時引数では切り替えない。
var (
	appDataDirectoryName    = "widget-go"
	singleInstanceMutexName = "Global\\widget-go-single-instance-mutex"
	applicationWindowTitle  = "システムモニター"
	distributionChannel     = "portable"
)
