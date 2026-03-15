package httpclient

import (
    "net"
    "net/http"
    "time"
)

func New(timeout int) *http.Client {

    tr := &http.Transport{
        MaxIdleConns:        512,
        MaxIdleConnsPerHost: 128,
        IdleConnTimeout:     60 * time.Second,
        DialContext: (&net.Dialer{
            Timeout: 20 * time.Second,
        }).DialContext,
    }

    return &http.Client{
        Transport: tr,
        Timeout:   time.Duration(timeout) * time.Second,
    }
}
