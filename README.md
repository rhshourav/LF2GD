# LF2GD (Large File From GitHub Downloader)

**Developed by [@rhshourav**](https://github.com/rhshourav)

**LF2GD** is a high-performance, multi-threaded downloader written in Go. It is specifically designed to handle large files split into multiple parts (like `.part001.exe`, `.part002.rar`) hosted on GitHub or other raw file servers.

### Key Features

* 🚀 **Multi-threaded Worker Pool**: Download multiple file parts simultaneously.
* 📊 **Beautiful TUI**: Real-time progress tracking and speed monitoring using `Bubble Tea`.
* 🔄 **Smart Resume**: Detects existing bytes and resumes interrupted downloads automatically.
* 🛠️ **Auto-Joiner**: Automatically merges all downloaded parts into a single final executable.
* 🛡️ **Retry Logic**: Robust handling of network flakiness.

---

## ⚙️ Configuration (`config.json`)

The software is driven by a `config.json` file. Create this in the root directory of the project:

```json
{
  "author": "rhshourav",
  "github": "https://github.com/rhshourav",
  "base_url": "https://raw.githubusercontent.com/user/repo/main/files",
  "download_path": "./downloads",
  "threads": 8,
  "retry_count": 5,
  "total_parts": 113
}

```

### Parameters:

| Key | Description |
| --- | --- |
| `base_url` | The URL prefix where the parts are hosted (without the filename). |
| `download_path` | The local folder where parts and the final file will be saved. |
| `threads` | Number of simultaneous downloads (Workers). |
| `total_parts` | The total number of split files to fetch (e.g., 113). |

---

## 🛠️ How to Build and Run

### Prerequisites

* [Go 1.22+](https://go.dev/dl/)

### 1. Clone the repository

```bash
git clone https://github.com/rhshourav/LF2GD.git
cd LF2GD

```

### 2. Initialize and Install Dependencies

```bash
go mod init downloader
go mod tidy

```

### 3. Build and Run

To run the software immediately:

```bash
go run main.go

```

To build a standalone executable:

```bash
# Windows
go build -o LF2GD.exe main.go

# Linux/macOS
go build -o LF2GD main.go

```

---

## 📂 Project Structure

* `main.go`: The core engine (Downloader, UI, and Joiner).
* `config.json`: User-defined settings.
* `go.mod`: Dependency management.
* `downloads/`: Default directory for output (auto-created).

---

## 🤝 Contributing

Contributions are welcome! Feel free to open an issue or submit a pull request.

**Author:** [rhshourav](https://github.com/rhshourav)
