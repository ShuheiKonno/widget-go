module widget-go

go 1.26.10

require (
	github.com/go-ole/go-ole v1.3.0
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/shirou/gopsutil/v4 v4.26.6
	golang.org/x/image v0.48.0
	golang.org/x/sys v0.50.0
)

require (
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/lufia/plan9stats v0.0.0-20260627054121-477a66015f15 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
)

replace github.com/jchv/go-webview2 => ./third_party/go-webview2
