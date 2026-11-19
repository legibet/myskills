module github.com/legibet/myskills

go 1.27.3

// Junctions, which myskills makes for links on Windows, report as symlinks, so
// path resolution and directory walks follow them like symlinks elsewhere.
godebug winsymlink=0

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.2
	charm.land/glamour/v2 v2.0.1
	charm.land/lipgloss/v2 v2.1.1
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/x/ansi v0.11.10
	github.com/charmbracelet/x/term v0.2.2
	github.com/egoist/mygo v0.10.0
	github.com/sahilm/fuzzy v0.1.3
	github.com/spf13/cobra v1.10.2
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/sys v0.50.0
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261119104433-f0c8309c9960 // indirect
	github.com/charmbracelet/x/exp/slice v0.1.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/dlclark/regexp2/v2 v2.13.1 // indirect
	github.com/ebitengine/purego v0.11.3 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.1.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/xo/terminfo v1.3.1 // indirect
	github.com/yuin/goldmark v1.8.7 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo
