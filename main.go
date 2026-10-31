package main

import (
	"os"

	"github.com/legibet/myskills/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
