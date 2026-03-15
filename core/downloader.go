package core

import (
	"downloader/config"
	"downloader/resume"
	"net/http"
	"sync"
	"time"
)

type Downloader struct {
	cfg    config.Config
	client *http.Client
	db     *resume.DB
	jobs   []*FileJob
}

func NewDownloader(
	cfg config.Config,
	client *http.Client,
	db *resume.DB,
	jobs []*FileJob,
) *Downloader {

	return &Downloader{
		cfg:    cfg,
		client: client,
		db:     db,
		jobs:   jobs,
	}
}

func (d *Downloader) Start() {

	fileSem := make(chan struct{}, d.cfg.ConcurrentFiles)

	var wg sync.WaitGroup

	for _, job := range d.jobs {

		wg.Add(1)

		go func(j *FileJob) {

			defer wg.Done()

			fileSem <- struct{}{}
			defer func() { <-fileSem }()

			d.downloadFile(j)

		}(job)
	}

	wg.Wait()
}

// core/downloader.go
func (d *Downloader) downloadFile(job *FileJob) {
	// Basic fix: print that it started
	// In a real scenario, you'd use d.client.Get(job.URL) here
	time.Sleep(2 * time.Second)
}
