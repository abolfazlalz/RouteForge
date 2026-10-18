package cli

import "embed"

//go:embed bundle/**
var runtimeSource embed.FS
