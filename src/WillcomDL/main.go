package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

var (
	totalDownloaded int64
	completedFiles  int64
	isVerifying     bool
	verifyResult    string
	startTime       time.Time
	endTime         time.Time
	finished        bool
	peakMu          sync.Mutex
	peakSpeed       float64
)

type Config struct {
	Author       string
	Github       string
	BaseURL      string
	DownloadPath string
	Threads      int
	RetryCount   int
	TotalParts   int
}

type model struct {
	progress progress.Model
	speed    float64
	total    int
	lastByte int64
	dots     int
}

func loadConfig() Config {
	return Config{
		Author:       "rhshourav",
		Github:       "https://github.com/rhshourav",
		BaseURL:      "https://raw.githubusercontent.com/rhshourav/ideal-fishstick/refs/heads/main/Willcom%20E4",
		DownloadPath: "./downloads",
		Threads:      8,
		RetryCount:   5,
		TotalParts:   179,
	}
}

func generateFiles(cfg Config) []string {
	files := []string{"Willcom E4.part001.exe"}
	for i := 2; i <= cfg.TotalParts; i++ {
		files = append(files, fmt.Sprintf("Willcom E4.part%03d.rar", i))
	}
	return files
}

func downloadFile(cfg Config, name string) error {
	path := filepath.Join(cfg.DownloadPath, name)

	escapedName := url.PathEscape(name)
	downloadURL := cfg.BaseURL + "/" + escapedName

	var start int64
	if info, err := os.Stat(path); err == nil {
		start = info.Size()
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("request error: %v", err)
	}

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

type finishedMsg struct{}

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

	hashes := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hashes[parts[0]] = parts[1]
		}
	}

	for _, name := range files {
		expectedHash, exists := hashes[name]
		if !exists {
			continue
		}

		path := filepath.Join(cfg.DownloadPath, name)
		f, err := os.Open(path)
		if err != nil {
			verifyResult = fmt.Sprintf("Verification failed: Could not read %s", name)
			return
		}

		h := sha256.New()
		io.Copy(h, f)
		f.Close()

		if hex.EncodeToString(h.Sum(nil)) != expectedHash {
			verifyResult = fmt.Sprintf("Verification failed: SHA-256 mismatch on %s", name)
			return
		}
	}
	verifyResult = "Verification successful! All files match SHA-256 hashes."
}

func startWorkflow(cfg Config, p *tea.Program) {
	startTime = time.Now()
	os.MkdirAll(cfg.DownloadPath, 0755)
	files := generateFiles(cfg)
	jobs := make(chan string, len(files))
	var wg sync.WaitGroup

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

	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	verifyFiles(cfg, files)

	endTime = time.Now()
	finished = true
	p.Send(finishedMsg{})
}

func (m model) Init() tea.Cmd { return tick() }
func tick() tea.Cmd           { return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg { return t }) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		if finished {
			return m, tea.Quit
		}
		return m, tea.Quit
	}
	if _, ok := msg.(time.Time); ok {
		curr := atomic.LoadInt64(&totalDownloaded)
		m.speed = float64(curr-m.lastByte) * 2 / 1024 / 1024
		m.lastByte = curr
		peakMu.Lock()
		if m.speed > peakSpeed {
			peakSpeed = m.speed
		}
		peakMu.Unlock()
		m.dots = (m.dots + 1) % 4
		return m, tick()
	}
	if _, ok := msg.(finishedMsg); ok {
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	done := atomic.LoadInt64(&completedFiles)
	status := "Downloading"
	if isVerifying {
		status = "Verifying Hashes"
	}

	dots := strings.Repeat(".", m.dots)

	info := fmt.Sprintf("%s\nStatus: %s%s\nFiles:  %d/%d\nSpeed:  %.2f MB/s",
		titleStyle.Render("Willcom Downloader (LF2GD)"), status, dots, done, m.total, m.speed)

	bar := m.progress.ViewAs(float64(done) / float64(m.total))

	if finished {
		dur := endTime.Sub(startTime)
		totalMB := float64(atomic.LoadInt64(&totalDownloaded)) / 1024 / 1024
		avg := 0.0
		if dur.Seconds() > 0 {
			avg = totalMB / dur.Seconds()
		}
		peakMu.Lock()
		p := peakSpeed
		peakMu.Unlock()

		summary := fmt.Sprintf("\n--- Summary ---\nTime elapsed: %s\nTotal downloaded: %.2f MB\nAverage speed: %.2f MB/s\nPeak speed: %.2f MB/s\nFiles completed: %d/%d\nVerification: %s\n\nPress any key to exit.", formatDuration(dur), totalMB, avg, p, done, m.total, verifyResult)

		return fmt.Sprintf("\n%s\n\n%s\n%s", info, bar, summary)
	}

	return fmt.Sprintf("\n%s\n\n%s\n", info, bar)
}

func formatDuration(d time.Duration) string {
	if d.Hours() >= 1 {
		return fmt.Sprintf("%02dh%02dm%02ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	if d.Minutes() >= 1 {
		return fmt.Sprintf("%02dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%02ds", int(d.Seconds()))
}

func main() {
	cfg := loadConfig()
	m := model{progress: progress.New(progress.WithDefaultGradient()), total: cfg.TotalParts}
	p := tea.NewProgram(m)
	go startWorkflow(cfg, p)
	p.Run()
	fmt.Printf("\n--- Process Finished ---\n%s\n", verifyResult)
}
