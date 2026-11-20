package main

import "Canyou/internal/desktop"

// Version 在构建时经 -X main.Version 注入。
var Version = "dev"

func main() { desktop.Run(Version) }
