<div align="center">
  <h1>🕹️ ~ Cartridge</h1>
  <p>All emulators just here</p>
</div>

<p align="center">
  <a href="https://github.com/IwnuplyNotTyan/Cartridge/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/IwnuplyNotTyan/Cartridge/ci.yml" alt="Build Status"/>
  </a>
  <img src="https://img.shields.io/github/license/IwnuplyNotTyan/Cartridge" alt="License"/>
  <img src="https://img.shields.io/github/stars/IwnuplyNotTyan/Cartridge" alt="Stars"/>
  <img src="https://img.shields.io/github/last-commit/IwnuplyNotTyan/Cartridge" alt="Last Commit"/>
</p>

<p align="center">
  <img src="https://github.com/IwnuplyNotTyan/Cartridge/blob/main/.github/assets/screenshot.png?raw=true" alt="Screenshot">
</p>

---

# 💖 Usage

Write `~/.config/cartridge/recipes.json` or a recipes directory (one recipe per JSON file - just drop a downloaded file in), examples can be find [here](https://github.com/iwnuplynottyan/cartridge/tree/master/_examples)

Add games, mods (aka hack's) and cheats, install emulators and then play!


For CLI usage, check `--help`

---

# 🪻 Install

> [!IMPORTANT]
> Select ANSI OSC52[^1] as clipboard in TMUX

[^1]: See [tmux clipboard](https://github.com/tmux/tmux/wiki/Clipboard)

## ❄️ Nix
``` bash
nix run github:iwnuplynottyan/cartridge
```


## ⛏️ Build from source
```sh
git clone https://github.com/IwnuplyNotTyan/Cartridge && cd Cartridge
go mod download
go build -o ./bin/cartridge main.go
```

---

## 👩‍💻 Author
- [iwnuplynottyan](https://github.com/iwnuplynottyan)

---

## 🛠️ Libraries Used
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework, core architecture
    - [Bubbles](https://github.com/charmbracelet/bubbles) — Modular widgets/components
    - [Charmtone](github.com/charmbracelet/x/exp/charmtone) — Color Pallet 
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Log](https://github.com/charmbracelet/log) — Pretty logs
- [Go Osc52](github.com/aymanbagabas/go-osc52/v2) — Copy text to clipboard
- [Cobra](https://github.com/spf13/cobra) — CLI commands and flags
- [Fang](https://github.com/charmbracelet/fang) — Pretty help, errors and version

---

## 📄 License
[MIT](https://github.com/IwnuplyNotTyan/Hera/blob/main/LICENSE).

<div align="center">
  <h1>Made with ❤️ </h1>
</div>
