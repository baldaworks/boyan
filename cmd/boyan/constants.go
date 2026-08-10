package main

import "time"

const (
	defaultStateDir                       = "./.state/boyan"
	defaultHTTPListenAddress              = "127.0.0.1:8686"
	defaultHTTPMaxBodyBytes         int64 = 1 << 20
	defaultHTTPRequestTimeout             = 15 * time.Second
	defaultShutdownTimeout                = 10 * time.Second
	defaultDerivationTimeout              = 30 * time.Second
	defaultDerivationMaxOutputBytes       = 262144
	defaultCanonicalGCInterval            = 6 * time.Hour
	defaultCanonicalDiscard               = 0.5
)

const (
	boyanCanonicalDirName  = "badger"
	boyanProjectionDirName = "bleve"
	boyanProjectionID      = "bleve"
)
