package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Global state for tracking progress across goroutines
var (
	totalDownloaded int64
	completedFiles  int64
	isJoining       bool
	isVerifying     bool
	verifyResult    string
)

type Config struct {
	Author       string `json:"author"`
	Github       string `json:"github"`
	BaseURL      string `json:"base_url"`
	DownloadPath string `json:"download_path"`
	Threads      int    `json:"threads"`
	RetryCount   int    `json:"retry_count"`
	TotalParts   int    `json:"total_parts"`
}

type model struct {
	progress progress.Model
	speed    float64
	total    int
	lastByte int64
}

func loadConfig() Config {
	file, err := os.Open("config.json")
	if err != nil {
		panic("Error: config.json not found.")
	}
	defer file.Close()
	var cfg Config
	json.NewDecoder(file).Decode(&cfg)
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return cfg
}

func generateFiles(cfg Config) []string {
	files := []string{"SystemPE.part001.exe"}
	for i := 2; i <= cfg.TotalParts; i++ {
		files = append(files, fmt.Sprintf("SystemPE.part%03d.rar", i))
	}
	return files
}

func downloadFile(cfg Config, name string) error {
	path := filepath.Join(cfg.DownloadPath, name)
	url := cfg.BaseURL + "/" + name

	var start int64
	if info, err := os.Stat(path); err == nil {
		start = info.Size()
	}

	req, _ := http.NewRequest("GET", url, nil)
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode >= 400 {
		return fmt.Errorf("network error")
	}
	defer resp.Body.Close()

	var file *os.File
	if start > 0 && resp.StatusCode == http.StatusPartialContent {
		file, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		file, _ = os.Create(path)
	}
	defer file.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			file.Write(buf[:n])
			atomic.AddInt64(&totalDownloaded, int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	atomic.AddInt64(&completedFiles, 1)
	return nil
}

func verifyFiles(cfg Config, files []string) {
	isVerifying = true
	defer func() { isVerifying = false }()

	url := cfg.BaseURL + "/HASH"
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		verifyResult = "Sorry, could not verify (HASH file not found on server)."
		return
	}
	defer resp.Body.Close()

	// Parse the HASH file into a map
	hashes := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hashes[parts[0]] = parts[1] // filename -> hash
		}
	}

	// Verify downloaded files against the map
	for _, name := range files {
		expectedHash, exists := hashes[name]
		if !exists {
			continue // Skip if the file isn't listed in the HASH file
		}

		path := filepath.Join(cfg.DownloadPath, name)
		f, err := os.Open(path)
		if err != nil {
			verifyResult = fmt.Sprintf("Verification failed: Could not read %s", name)
			return
		}

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			verifyResult = fmt.Sprintf("Verification failed: Error hashing %s", name)
			return
		}
		f.Close()

		actualHash := hex.EncodeToString(h.Sum(nil))
		if actualHash != expectedHash {
			verifyResult = fmt.Sprintf("Verification failed: SHA-256 mismatch on %s", name)
			return
		}
	}

	verifyResult = "Verification successful! All files match SHA-256 hashes."
}

func joinFiles(cfg Config) error {
	isJoining = true
	finalPath := filepath.Join(cfg.DownloadPath, "SystemPE_Full.exe")
	out, err := os.Create(finalPath)
	if err != nil {
		return err
	}
	defer out.Close()

	for _, name := range generateFiles(cfg) {
		in, err := os.Open(filepath.Join(cfg.DownloadPath, name))
		if err != nil {
			return err
		}
		io.Copy(out, in)
		in.Close()
	}
	return nil
}

func startWorkflow(cfg Config, p *tea.Program) {
	os.MkdirAll(cfg.DownloadPath, 0755)
	files := generateFiles(cfg)
	jobs := make(chan string, len(files))
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < cfg.Threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				for r := 0; r < cfg.RetryCount; r++ {
					if err := downloadFile(cfg, name); err == nil {
						break
					}
					time.Sleep(time.Second)
				}
			}
		}()
	}

	// Send jobs
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait() // Wait for all downloads to finish

	// Step 2: Verify Files
	verifyFiles(cfg, files)

	// Step 3: Join Files (Only if verification didn't explicitly fail due to a mismatch)
	if !strings.Contains(verifyResult, "Verification failed:") {
		joinFiles(cfg)
	}

	p.Quit()
}

func (m model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
		return t
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case time.Time:
		curr := atomic.LoadInt64(&totalDownloaded)
		m.speed = float64(curr-m.lastByte) * 2 / 1024 / 1024
		m.lastByte = curr
		return m, tick()
	}
	return m, nil
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	doneCount := atomic.LoadInt64(&completedFiles)

	status := "Downloading..."
	if isVerifying {
		status = "Verifying SHA-256 Hashes... Please wait."
	} else if isJoining {
		status = "Merging Files... Please wait."
	}

	info := fmt.Sprintf(
		"%s\nStatus: %s\nFiles:  %d/%d\nSpeed:  %.2f MB/s\nTotal:  %.2f MB",
		titleStyle.Render("Large File From Github Downloader(LF2GD)"),
		status,
		doneCount,
		m.total,
		m.speed,
		float64(m.lastByte)/1024/1024,
	)

	return fmt.Sprintf("\n%s\n\n%s\n\n(Press q to quit)\n", info, m.progress.ViewAs(float64(doneCount)/float64(m.total)))
}

func main() {
	cfg := loadConfig()

	m := model{
		progress: progress.New(progress.WithDefaultGradient()),
		total:    cfg.TotalParts,
	}

	p := tea.NewProgram(m)

	go startWorkflow(cfg, p)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Print final results
	fmt.Println("\n--- Tasks Complete ---")
	fmt.Println(verifyResult)
	if !strings.Contains(verifyResult, "Verification failed:") {
		fmt.Println("Success! Final file created in:", cfg.DownloadPath)
	} else {
		fmt.Println("Process aborted due to file corruption.")
	}
}
