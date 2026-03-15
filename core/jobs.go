package core

import (
    "downloader/config"
    "fmt"
    "net/http"
)

func BuildJobs(cfg config.Config, client *http.Client) []*FileJob {

    var jobs []*FileJob

    base := cfg.BaseURL

    jobs = append(jobs, &FileJob{
        Name: "SystemPE.part001.exe",
        URL: base + "SystemPE.part001.exe",
    })

    for i := 2; i <= cfg.TotalParts; i++ {

        name := fmt.Sprintf("SystemPE.part%03d.rar", i)

        jobs = append(jobs, &FileJob{
            Name: name,
            URL: base + name,
        })
    }

    return jobs
}
