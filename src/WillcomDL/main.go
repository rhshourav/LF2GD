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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Global metrics
var (
	totalDownloaded   int64
	completedFiles    int64
	isVerifying       bool
	verifyResult      string
	startTime         time.Time
	endTime           time.Time
	finished          bool
	peakSpeed         float64
	speedSamples      []float64
	speedMu           sync.Mutex
	resumedBytes      int64
	freshBytes        int64
	activeConnections int64 // currently active HTTP connections (segments)
)

// Config holds runtime options
type Config struct {
	Author          string
	Github          string
	BaseURL         string
	DownloadPath    string
	Threads         int // number of concurrent file workers
	RetryCount      int // per-segment retry attempts
	TotalParts      int
	SegmentsPerFile int // connections per file
	MaxConnections  int // global limit of simultaneous HTTP connections
	MovingAvgWindow int // window for ETA moving average (samples)
}

// FileState tracks per-file progress and metadata
type FileState struct {
	name       string
	total      int64 // file total size (0 if unknown)
	downloaded int64 // bytes downloaded for this file
}

// SegmentState is optional internal structure but not needed externally

// model is the Bubble Tea UI model
type model struct {
	progress        progress.Model
	speed           float64
	total           int
	lastByte        int64
	spinnerIndex    int
	page            int
	pageSize        int
	movingAvgWindow int
	files           []*FileState
	fsMu            sync.Mutex
}

func loadConfig() Config {
	return Config{
		Author:          "rhshourav",
		Github:          "https://github.com/rhshourav",
		BaseURL:         "https://raw.githubusercontent.com/rhshourav/ideal-fishstick/refs/heads/main/Willcom%20E4",
		DownloadPath:    "./downloads",
		Threads:         4,
		RetryCount:      5,
		TotalParts:      179,
		SegmentsPerFile: 4,
		MaxConnections:  24,
		MovingAvgWindow: 12,
	}
}

func generateFiles(cfg Config) []string {
	files := []string{"Willcom E4.part001.exe"}
	for i := 2; i <= cfg.TotalParts; i++ {
		files = append(files, fmt.Sprintf("Willcom E4.part%03d.rar", i))
	}
	return files
}

// probeRemoteSize tries HEAD then range requests to find remote file size
func probeRemoteSize(cfg Config, name string) int64 {
	escapedName := url.PathEscape(name)
	downloadURL := cfg.BaseURL + "/" + escapedName

	req, _ := http.NewRequest("HEAD", downloadURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if val, err := strconv.ParseInt(cl, 10, 64); err == nil {
				return val
			}
		}
	}

	// Fallback: try a tiny range request to get Content-Range
	req2, _ := http.NewRequest("GET", downloadURL, nil)
	req2.Header.Set("Range", "bytes=0-0")
	resp2, err2 := http.DefaultClient.Do(req2)
	if err2 == nil && resp2 != nil {
		defer resp2.Body.Close()
		cr := resp2.Header.Get("Content-Range")
		if cr != "" { // format "bytes 0-0/12345"
			parts := strings.Split(cr, "/")
			if len(parts) == 2 {
				if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					return total
				}
			}
		}
		if cl := resp2.Header.Get("Content-Length"); cl != "" {
			if val, err := strconv.ParseInt(cl, 10, 64); err == nil {
				return val
			}
		}
	}
	return 0
}

// parseTotalSize helper used for GET responses
func parseTotalSize(resp *http.Response, start int64) int64 {
	cr := resp.Header.Get("Content-Range")
	if cr != "" {
		parts := strings.Split(cr, "/")
		if len(parts) == 2 {
			if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				return total
			}
		}
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if val, err := strconv.ParseInt(cl, 10, 64); err == nil {
			if resp.StatusCode == http.StatusPartialContent {
				return start + val
			}
			return val
		}
	}
	return 0
}

// downloadSegment downloads a byte range for a file into a temp segment file.
// It supports resume for the particular segment temp file and retries (per-segment).
func downloadSegment(cfg Config, fs *FileState, segIndex int, segStart, segEnd int64, connTokens chan struct{}) error {
	// Acquire a global connection token
	connTokens <- struct{}{}
	atomic.AddInt64(&activeConnections, 1)
	defer func() {
		<-connTokens
		atomic.AddInt64(&activeConnections, -1)
	}()

	segPath := filepath.Join(cfg.DownloadPath, fmt.Sprintf("%s.seg%03d.tmp", fs.name, segIndex))
	var existing int64
	if info, err := os.Stat(segPath); err == nil {
		existing = info.Size()
		// if file already complete for this segment, skip
		if segStart+existing > segEnd {
			atomic.AddInt64(&totalDownloaded, existing)
			atomic.AddInt64(&fs.downloaded, existing)
			return nil
		}
	}

	// prepare request for remaining range
	reqStart := segStart + existing
	client := http.DefaultClient

	urlName := cfg.BaseURL + "/" + url.PathEscape(fs.name)
	try := 0
	for {
		try++
		req, _ := http.NewRequest("GET", urlName, nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", reqStart, segEnd))

		resp, err := client.Do(req)
		if err != nil || resp == nil || resp.StatusCode >= 400 {
			if try >= cfg.RetryCount {
				if resp != nil {
					if resp.Body != nil {
						resp.Body.Close()
					}
				}
				return fmt.Errorf("segment %d: network error after %d attempts: %v", segIndex, try, err)
			}
			time.Sleep(time.Second)
			continue
		}

		// open file for append
		f, err := os.OpenFile(segPath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			resp.Body.Close()
			return err
		}
		if existing > 0 {
			if _, err := f.Seek(existing, io.SeekStart); err != nil {
				f.Close()
				resp.Body.Close()
				return err
			}
		}

		buf := make([]byte, 32*1024)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				nw, werr := f.Write(buf[:n])
				if nw > 0 {
					atomic.AddInt64(&totalDownloaded, int64(nw))
					atomic.AddInt64(&fs.downloaded, int64(nw))
				}
				if werr != nil {
					f.Close()
					resp.Body.Close()
					return werr
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				resp.Body.Close()
				// retry logic
				if try >= cfg.RetryCount {
					return fmt.Errorf("segment %d: read error: %v", segIndex, rerr)
				}
				// backoff and retry (will request remaining bytes)
				resp.Body.Close()
				time.Sleep(time.Second * time.Duration(try))
				existing2 := int64(0)
				if info2, err := os.Stat(segPath); err == nil {
					existing2 = info2.Size()
				}
				existing = existing2
				reqStart = segStart + existing
				break
			}
		}
		f.Close()
		resp.Body.Close()
		// Check if segment reached expected size
		info, err := os.Stat(segPath)
		if err == nil {
			got := info.Size()
			expected := segEnd - segStart + 1
			if int64(got) >= expected {
				// done
				return nil
			}
			// otherwise, loop again to fetch remaining
			existing = got
			reqStart = segStart + existing
			if try >= cfg.RetryCount {
				return fmt.Errorf("segment %d: incomplete after retries", segIndex)
			}
			// small pause and retry
			time.Sleep(time.Second)
			continue
		}
		// if file missing for some reason, retry
		if try >= cfg.RetryCount {
			return fmt.Errorf("segment %d: failed to stat temp file", segIndex)
		}
		time.Sleep(time.Second)
	}
}

// mergeSegments concatenates segment temp files into the final file (atomically).
func mergeSegments(cfg Config, fs *FileState, segCount int) error {
	finalPath := filepath.Join(cfg.DownloadPath, fs.name)
	tmpFinal := finalPath + ".parttmp"
	out, err := os.Create(tmpFinal)
	if err != nil {
		return err
	}
	for i := 0; i < segCount; i++ {
		segPath := filepath.Join(cfg.DownloadPath, fmt.Sprintf("%s.seg%03d.tmp", fs.name, i))
		in, err := os.Open(segPath)
		if err != nil {
			out.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			return err
		}
		in.Close()
	}
	out.Close()
	// replace final file
	if err := os.Rename(tmpFinal, finalPath); err != nil {
		return err
	}
	// cleanup segments
	for i := 0; i < segCount; i++ {
		segPath := filepath.Join(cfg.DownloadPath, fmt.Sprintf("%s.seg%03d.tmp", fs.name, i))
		os.Remove(segPath)
	}
	return nil
}

// downloadFileMulti handles per-file segmented download
func downloadFileMulti(cfg Config, fs *FileState, connTokens chan struct{}) error {
	// If server doesn't expose size, try probe again
	if atomic.LoadInt64(&fs.total) == 0 {
		size := probeRemoteSize(cfg, fs.name)
		if size > 0 {
			atomic.StoreInt64(&fs.total, size)
		}
	}
	total := atomic.LoadInt64(&fs.total)
	// If we still don't know size, fallback to single-stream download
	if total <= 0 || cfg.SegmentsPerFile <= 1 {
		// single-stream (non-segmented) download with resume/Range support
		escaped := url.PathEscape(fs.name)
		u := cfg.BaseURL + "/" + escaped
		path := filepath.Join(cfg.DownloadPath, fs.name)
		var start int64
		if info, err := os.Stat(path); err == nil {
			start = info.Size()
			atomic.AddInt64(&resumedBytes, start)
		}
		try := 0
		for {
			try++
			req, _ := http.NewRequest("GET", u, nil)
			if start > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil || resp == nil || resp.StatusCode >= 400 {
				if try >= cfg.RetryCount {
					if resp != nil && resp.Body != nil {
						resp.Body.Close()
					}
					return fmt.Errorf("single-stream network error: %v", err)
				}
				time.Sleep(time.Second)
				continue
			}
			// set total if possible
			if t := parseTotalSize(resp, start); t > 0 {
				atomic.StoreInt64(&fs.total, t)
			}
			var f *os.File
			if start > 0 && resp.StatusCode == http.StatusPartialContent {
				f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
			} else {
				f, _ = os.Create(path)
			}
			buf := make([]byte, 32*1024)
			for {
				n, rerr := resp.Body.Read(buf)
				if n > 0 {
					nw, werr := f.Write(buf[:n])
					if nw > 0 {
						atomic.AddInt64(&totalDownloaded, int64(nw))
						atomic.AddInt64(&fs.downloaded, int64(nw))
					}
					if werr != nil {
						f.Close()
						resp.Body.Close()
						return werr
					}
				}
				if rerr == io.EOF {
					break
				}
				if rerr != nil {
					f.Close()
					resp.Body.Close()
					if try >= cfg.RetryCount {
						return rerr
					}
					startInfo := int64(0)
					if info2, err := os.Stat(path); err == nil {
						startInfo = info2.Size()
					}
					start = startInfo
					time.Sleep(time.Second * time.Duration(try))
					break
				}
			}
			f.Close()
			resp.Body.Close()
			atomic.AddInt64(&freshBytes, atomic.LoadInt64(&fs.total)-start)
			atomic.AddInt64(&completedFiles, 1)
			return nil
		}
	}

	// Segmented download path
	segCount := cfg.SegmentsPerFile
	segmentSize := total / int64(segCount)
	var wg sync.WaitGroup
	errCh := make(chan error, segCount)
	for i := 0; i < segCount; i++ {
		wg.Add(1)
		segStart := int64(i) * segmentSize
		var segEnd int64
		if i == segCount-1 {
			segEnd = total - 1
		} else {
			segEnd = segStart + segmentSize - 1
		}
		go func(idx int, sStart, sEnd int64) {
			defer wg.Done()
			segTry := 0
			for {
				segTry++
				if err := downloadSegment(cfg, fs, idx, sStart, sEnd, connTokens); err != nil {
					if segTry >= cfg.RetryCount {
						errCh <- fmt.Errorf("file %s segment %d failed: %v", fs.name, idx, err)
						return
					}
					time.Sleep(time.Second * time.Duration(segTry))
					continue
				}
				// success
				return
			}
		}(i, segStart, segEnd)
	}
	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		// if any segment failed after retries, return error
		return err
	}

	// All segments downloaded: merge them
	if err := mergeSegments(cfg, fs, segCount); err != nil {
		return err
	}
	atomic.AddInt64(&completedFiles, 1)
	atomic.AddInt64(&freshBytes, total)
	return nil
}

// verifyFiles loads HASH and verifies SHA256 per-file (if available)
func verifyFiles(cfg Config, files []string) {
	isVerifying = true
	defer func() { isVerifying = false }()

	url := cfg.BaseURL + "/HASH"
	resp, err := http.Get(url)
	if err != nil || resp == nil || resp.StatusCode != 200 {
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
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			verifyResult = fmt.Sprintf("Verification failed: Could not read %s", name)
			return
		}
		f.Close()
		if hex.EncodeToString(h.Sum(nil)) != expectedHash {
			verifyResult = fmt.Sprintf("Verification failed: SHA-256 mismatch on %s", name)
			return
		}
	}
	verifyResult = "Verification successful! All files match SHA-256 hashes."
}

// startWorkflow orchestrates file-worker goroutines and overall download lifecycle
func startWorkflow(cfg Config, p *tea.Program, m *model) {
	startTime = time.Now()
	os.MkdirAll(cfg.DownloadPath, 0755)

	// Pre-probe sizes (best-effort)
	for _, fs := range m.files {
		if atomic.LoadInt64(&fs.total) == 0 {
			if size := probeRemoteSize(cfg, fs.name); size > 0 {
				atomic.StoreInt64(&fs.total, size)
			}
		}
	}

	// global connection token bucket to limit concurrent HTTP connections
	connTokens := make(chan struct{}, cfg.MaxConnections)

	// job queue for files
	jobs := make(chan *FileState, len(m.files))
	var wg sync.WaitGroup

	// start file worker goroutines
	for i := 0; i < cfg.Threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fs := range jobs {
				// attempt per-file download with retries
				try := 0
				for {
					try++
					if err := downloadFileMulti(cfg, fs, connTokens); err == nil {
						// success
						break
					} else {
						// log / backoff
						if try >= cfg.RetryCount {
							// give up on this file after retries (still continue others)
							fmt.Fprintf(os.Stderr, "file %s failed after retries: %v\n", fs.name, err)
							break
						}
						time.Sleep(time.Second * time.Duration(try))
					}
				}
			}
		}()
	}

	// enqueue files
	for _, fs := range m.files {
		jobs <- fs
	}
	close(jobs)
	wg.Wait()

	// verification (optional, best-effort)
	verifyFiles(cfg, generateFiles(cfg))

	endTime = time.Now()
	finished = true
	p.Send(finishedMsg{})
}

// UI / Bubble Tea implementation

type finishedMsg struct{}

func (m model) Init() tea.Cmd { return tick() }
func tick() tea.Cmd           { return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg { return t }) }

func appendSpeedSample(s float64) {
	speedMu.Lock()
	if len(speedSamples) >= 120 {
		speedSamples = speedSamples[1:]
	}
	speedSamples = append(speedSamples, s)
	if s > peakSpeed {
		peakSpeed = s
	}
	speedMu.Unlock()
}

// movingAverage computes moving average over last `window` samples
func movingAverage(window int) float64 {
	speedMu.Lock()
	defer speedMu.Unlock()
	if len(speedSamples) == 0 {
		return 0
	}
	if window <= 0 || window > len(speedSamples) {
		window = len(speedSamples)
	}
	sum := 0.0
	for i := len(speedSamples) - window; i < len(speedSamples); i++ {
		sum += speedSamples[i]
	}
	return sum / float64(window)
}

func sparkline(samples []float64, width int) string {
	if len(samples) == 0 {
		return ""
	}
	min, max := samples[0], samples[0]
	for _, v := range samples {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	chars := []rune("▁▂▃▄▅▆▇█")
	out := make([]rune, 0, len(samples))
	for _, v := range samples {
		pos := 0
		if max > min {
			norm := (v - min) / (max - min)
			pos = int(norm * float64(len(chars)-1))
		}
		out = append(out, chars[pos])
	}
	if width <= 0 {
		return string(out)
	}
	if len(out) > width {
		return string(out[len(out)-width:])
	}
	if len(out) < width {
		padding := make([]rune, width-len(out))
		for i := range padding {
			padding[i] = ' '
		}
		return string(padding) + string(out)
	}
	return string(out)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if finished {
			return m, tea.Quit
		}
		if k == "ctrl+c" || k == "q" || k == "esc" {
			return m, tea.Quit
		}
		if k == "right" || k == "n" {
			maxPages := (m.total + m.pageSize - 1) / m.pageSize
			m.page = (m.page + 1) % maxPages
			return m, nil
		}
		if k == "left" || k == "p" {
			maxPages := (m.total + m.pageSize - 1) / m.pageSize
			m.page = (m.page - 1 + maxPages) % maxPages
			return m, nil
		}
		return m, nil
	case time.Time:
		curr := atomic.LoadInt64(&totalDownloaded)
		m.speed = float64(curr-m.lastByte) * 2 / 1024 / 1024
		m.lastByte = curr
		appendSpeedSample(m.speed)
		m.spinnerIndex = (m.spinnerIndex + 1) % 4
		return m, tick()
	case finishedMsg:
		return m, nil
	default:
		_ = msg
		return m, nil
	}
}

func renderBar(p float64, w int) string {
	if w < 1 {
		w = 20
	}
	filled := int(p * float64(w))
	if filled > w {
		filled = w
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("─", w-filled)
	return fmt.Sprintf("[%s] %3.0f%%", bar, p*100)
}

func formatBytes(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0f B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := b / float64(div)
	switch exp {
	case 0:
		return fmt.Sprintf("%.2f KB", value)
	case 1:
		return fmt.Sprintf("%.2f MB", value)
	case 2:
		return fmt.Sprintf("%.2f GB", value)
	default:
		return fmt.Sprintf("%.2f TB", value)
	}
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

func humanizeFileProgress(got, tot int64) string {
	if tot > 0 {
		return fmt.Sprintf("%s/%s", formatBytes(float64(got)), formatBytes(float64(tot)))
	}
	return fmt.Sprintf("%s/?", formatBytes(float64(got)))
}

func (m *model) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	done := atomic.LoadInt64(&completedFiles)
	status := "Downloading"
	if isVerifying {
		status = "Verifying Hashes"
	}

	spinnerChars := []rune{'|', '/', '-', '\\'}
	spinner := string(spinnerChars[m.spinnerIndex%len(spinnerChars)])

	info := fmt.Sprintf("%s\nStatus: %s %s\nFiles:  %d/%d\nSpeed:  %.2f MB/s",
		titleStyle.Render("Willcom Downloader (LF2GD)"), status, spinner, done, m.total, m.speed)

	bar := m.progress.ViewAs(float64(done) / float64(m.total))

	m.fsMu.Lock()
	totalFiles := len(m.files)
	m.fsMu.Unlock()

	if m.pageSize <= 0 {
		m.pageSize = 12
	}
	maxPages := (m.total + m.pageSize - 1) / m.pageSize
	if maxPages == 0 {
		maxPages = 1
	}
	if m.page < 0 || m.page >= maxPages {
		m.page = 0
	}

	startIdx := m.page * m.pageSize
	endIdx := startIdx + m.pageSize
	if endIdx > totalFiles {
		endIdx = totalFiles
	}

	var perFileLines []string
	m.fsMu.Lock()
	for i := startIdx; i < endIdx; i++ {
		fs := m.files[i]
		tot := atomic.LoadInt64(&fs.total)
		got := atomic.LoadInt64(&fs.downloaded)
		pct := 0.0
		if tot > 0 {
			pct = float64(got) / float64(tot)
		}
		name := fs.name
		if len(name) > 24 {
			name = name[:21] + "..."
		}
		line := fmt.Sprintf("%3d. %-24s %s %s", i+1, name, renderBar(pct, 28), humanizeFileProgress(got, tot))
		perFileLines = append(perFileLines, line)
	}
	m.fsMu.Unlock()

	speedMu.Lock()
	samplesCopy := append([]float64(nil), speedSamples...)
	p := peakSpeed
	speedMu.Unlock()

	window := m.movingAvgWindow
	if window <= 0 {
		window = 8
	}
	avgSpeed := movingAverage(window)

	totalRemote := int64(0)
	for _, fs := range m.files {
		totalRemote += atomic.LoadInt64(&fs.total)
	}
	remaining := float64(totalRemote - atomic.LoadInt64(&totalDownloaded))
	etaStr := "N/A"
	if avgSpeed > 0 {
		eta := time.Duration(remaining/(avgSpeed*1024*1024)) * time.Second
		etaStr = formatDuration(eta)
	}

	liveGraph := sparkline(samplesCopy, 40)

	resumeStats := fmt.Sprintf("ActiveConns: %d  Resumed: %s  Fresh: %s",
		atomic.LoadInt64(&activeConnections),
		formatBytes(float64(atomic.LoadInt64(&resumedBytes))),
		formatBytes(float64(atomic.LoadInt64(&freshBytes))),
	)

	pageInfo := fmt.Sprintf("Page %d/%d  (n/right → next, p/left ← prev, q → quit)", m.page+1, maxPages)

	if finished {
		dur := endTime.Sub(startTime)
		totalMB := float64(atomic.LoadInt64(&totalDownloaded)) / 1024 / 1024
		avg := 0.0
		if dur.Seconds() > 0 {
			avg = totalMB / dur.Seconds()
		}
		summary := fmt.Sprintf("\n--- Summary ---\nTime elapsed: %s\nTotal downloaded: %.2f MB\nAverage speed: %.2f MB/s\nPeak speed: %.2f MB/s\nFiles completed: %d/%d\nVerification: %s\n%s\n\nPress any key to exit.",
			formatDuration(dur), totalMB, avg, p, done, m.total, verifyResult, resumeStats)
		return fmt.Sprintf("\n%s\n\n%s\n\n%s\n\nBandwidth: %s  ETA: %s\n\n%s\n%s\n", info, bar, strings.Join(perFileLines, "\n"), liveGraph, etaStr, pageInfo, summary)
	}

	return fmt.Sprintf("\n%s\n\n%s\n\n%s\n\nBandwidth: %s  ETA: %s\n\n%s\n%s\n", info, bar, strings.Join(perFileLines, "\n"), liveGraph, etaStr, pageInfo, resumeStats)
}

func main() {
	cfg := loadConfig()
	// prepare model and file list
	m := &model{
		progress:        progress.New(progress.WithDefaultGradient()),
		total:           cfg.TotalParts,
		pageSize:        12,
		movingAvgWindow: cfg.MovingAvgWindow,
	}
	names := generateFiles(cfg)
	for _, n := range names {
		m.files = append(m.files, &FileState{name: n})
	}

	// Pre-create downloads directory
	os.MkdirAll(cfg.DownloadPath, 0755)

	// program
	p := tea.NewProgram(m)
	go startWorkflow(cfg, p, m)
	p.Run()
	fmt.Printf("\n--- Process Finished ---\n%s\n", verifyResult)
}
