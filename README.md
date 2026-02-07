# Custom Arduino Serial Monitor

Vibe-coded custom arduino serial monitor with csv output to save a little bit of time on other projects.

## Features
- COM port and baud rate selection
- Autoscroll and toggleable timestamps
- CSV export with time filtering and custom headers

## Build
```
go build -ldflags="-s -w" -o serial-monitor.exe .
```
