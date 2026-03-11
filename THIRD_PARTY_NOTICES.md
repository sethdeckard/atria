# Third-Party Notices

`atria` is licensed under the MIT License. Third-party components used by
`atria` remain under their respective licenses.

This notice file records the dependency licenses identified from the local Go
module graph during release preparation. Re-run the dependency license audit
before tagged releases to catch newly added or updated modules.

## MIT

- `github.com/BurntSushi/toml` `v1.6.0`
- `github.com/aymanbagabas/go-osc52/v2` `v2.0.1`
- `github.com/charmbracelet/bubbles` `v1.0.0`
- `github.com/charmbracelet/bubbletea` `v1.3.10`
- `github.com/charmbracelet/colorprofile` `v0.4.1`
- `github.com/charmbracelet/lipgloss` `v1.1.0`
- `github.com/charmbracelet/x/ansi` `v0.11.6`
- `github.com/charmbracelet/x/cellbuf` `v0.0.15`
- `github.com/charmbracelet/x/term` `v0.2.2`
- `github.com/clipperhouse/displaywidth` `v0.9.0`
- `github.com/clipperhouse/stringish` `v0.1.1`
- `github.com/clipperhouse/uax29/v2` `v2.5.0`
- `github.com/creack/pty` `v1.1.24`
- `github.com/erikgeiser/coninput` `v0.0.0-20211004153227-1c3628e74d0f`
- `github.com/hinshun/vt10x` `v0.0.0-20220301184237-5011da428d02`
- `github.com/lucasb-eyer/go-colorful` `v1.3.0`
- `github.com/mattn/go-isatty` `v0.0.20`
- `github.com/mattn/go-runewidth` `v0.0.19`
- `github.com/muesli/ansi` `v0.0.0-20230316100256-276c6243b2f6`
- `github.com/muesli/cancelreader` `v0.2.2`
- `github.com/muesli/termenv` `v0.16.0`
- `github.com/rivo/uniseg` `v0.4.7`
- `github.com/xo/terminfo` `v0.0.0-20220910002029-abceb7e1c41e`

## BSD-3-Clause

- `github.com/atotto/clipboard` `v0.1.4`
- `golang.org/x/sys` `v0.38.0`
- `golang.org/x/text` `v0.3.8`

## Audit Notes

Some transitive modules reported by `go list -m all` were not present in the
local module cache during the audit pass used to prepare this file. Those
modules should be rechecked before a packaged binary release if you need a
complete redistribution notice bundle.
