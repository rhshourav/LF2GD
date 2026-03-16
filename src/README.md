#  Downloader (LF2GD)

A multi-threaded terminal downloader written in **Go** for downloading large file sets split into multiple parts.
The program downloads each file with segmented HTTP range requests, displays real-time progress, and optionally verifies integrity using SHA-256 hashes.

---

# Features

* Multi-threaded downloading
* Segment-based parallel downloads
* Resume support
* Live progress UI (speed, ETA, progress bars)
* Automatic segment merging
* SHA-256 file verification
* Cross-platform builds (Windows, Linux, macOS)

---

# Requirements

* Go 1.20 or newer

Check your installation:

```
go version
```

---

# Project Structure

```
project/
│
├─ main.go
├─ build.ps1
├─ downloads/
└─ README.md
```

---

# Running the Program

Run directly from source:

```
go run main.go
```

Downloaded files will be stored in:

```
./downloads
```

---

# Building the Program

## Quick Build (Windows)

If you are building on Windows:

```
go build -o Downloader.exe main.go
```

---

# Cross-Platform Builds

Use the provided **PowerShell build script**.

Run:

```
.\build.ps1
```

The script automatically compiles binaries for multiple operating systems and architectures.

Generated output:

```
build/
 ├─ Downloader_windows_amd64.exe
 ├─ Downloader_windows_386.exe
 ├─ Downloader_windows_arm64.exe
 ├─ Downloader_linux_amd64
 ├─ Downloader_linux_arm64
 ├─ Downloader_macos_amd64
 └─ Downloader_macos_arm64
```

---

# Configuration

All configuration is located inside `loadConfig()` in **main.go**. And change the ```FILE_NAME```, ```NUMBER_OF_PARTS``` and ```FOLDER_NAME``` .

Example:

```go
func loadConfig() Config {
	return Config{
		Author:          "rhshourav",
		Github:          "https://github.com/rhshourav",
		BaseURL:         "https://raw.githubusercontent.com/rhshourav/ideal-fishstick/refs/heads/main/FOLDER_NAME",
		DownloadPath:    "./downloads",
		Threads:         4,
		RetryCount:      5,
		TotalParts:      "NUMBER_OF_PARTS",
		SegmentsPerFile: 4,
		MaxConnections:  24,
		MovingAvgWindow: 12,
	}
}
```

---

# Changing the Files Being Downloaded

The program uses a function called:

```
generateFiles()
```

Example:

```go
func generateFiles(cfg Config) []string {
	files := []string{"FILE_NAME.part001.exe"}
	for i := 2; i <= cfg.TotalParts; i++ {
		files = append(files, fmt.Sprintf("FILE_NAME.part%03d.rar", i))
	}
	return files
}
```
also
```go
info := fmt.Sprintf("%s\nStatus: %s %s\nFiles:  %d/%d\nSpeed:  %.2f MB/s",
		titleStyle.Render("FILE_NAME Downloader (LF2GD)"), status, spinner, done, m.total, m.speed)

	bar := m.progress.ViewAs(float64(done) / float64(m.total))

```
To download a different file set:

### Example structure

Server directory:

```
https://example.com/files/

file.part001.exe
file.part002.rar
file.part003.rar
file.part004.rar
```

Modify:

1. **BaseURL**

```
BaseURL: "https://example.com/files"
```

2. **TotalParts**

```
TotalParts: 4
```

3. **Filename format**

Adjust the naming format in `generateFiles()`.

Example if files are named:

```
archive_001.zip
archive_002.zip
archive_003.zip
```

Modify to:

```go
func generateFiles(cfg Config) []string {
	files := []string{}

	for i := 1; i <= cfg.TotalParts; i++ {
		files = append(files, fmt.Sprintf("archive_%03d.zip", i))
	}

	return files
}
```

---

# Adjusting Download Performance

You can tune performance parameters.

| Setting         | Description                         |
| --------------- | ----------------------------------- |
| Threads         | number of parallel files            |
| SegmentsPerFile | segments per file                   |
| MaxConnections  | total simultaneous HTTP connections |
| RetryCount      | retry attempts                      |

Example high-speed configuration:

```
Threads: 8
SegmentsPerFile: 8
MaxConnections: 64
```

---

# Hash Verification

If the server provides a file named:

```
HASH
```

Containing:

```
file.part001.exe SHA256HASH
file.part002.rar SHA256HASH
```

The downloader automatically verifies all files after download.

---

# Troubleshooting

### "This app can't run on your PC"

You compiled the wrong architecture.

Build the correct one:

```
windows 64-bit
GOARCH=amd64
```

or

```
windows 32-bit
GOARCH=386
```

---

### Slow download speed

Increase:

```
Threads
SegmentsPerFile
MaxConnections
```

---

# License

MIT License

---

# Author

GitHub: https://github.com/rhshourav
