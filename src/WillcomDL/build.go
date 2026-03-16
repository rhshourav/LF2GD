package main

import (
	"fmt"
	"os"
	"os/exec"
)

type target struct {
	goos   string
	goarch string
	out    string
}

func main() {
	targets := []target{
		{"windows", "amd64", "build/WillcomDownloader_windows_amd64.exe"},
		{"windows", "386", "build/WillcomDownloader_windows_386.exe"},
		{"windows", "arm64", "build/WillcomDownloader_windows_arm64.exe"},
		{"linux", "amd64", "build/WillcomDownloader_linux_amd64"},
		{"linux", "arm64", "build/WillcomDownloader_linux_arm64"},
		{"darwin", "amd64", "build/WillcomDownloader_macos_amd64"},
		{"darwin", "arm64", "build/WillcomDownloader_macos_arm64"},
	}

	os.MkdirAll("build", 0755)

	for _, t := range targets {
		fmt.Println("Building", t.goos, t.goarch)

		cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", t.out, "main.go")
		cmd.Env = append(os.Environ(),
			"GOOS="+t.goos,
			"GOARCH="+t.goarch,
		)

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}

	fmt.Println("All builds completed")
}
