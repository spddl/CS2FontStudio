# CS2 Font Studio
[![Downloads][1]][2] [![GitHub stars][3]][4]

[1]: https://img.shields.io/github/downloads/spddl/CS2FontStudio/total.svg
[2]: https://github.com/spddl/CS2FontStudio/releases "Downloads"

[3]: https://img.shields.io/github/stars/spddl/CS2FontStudio.svg
[4]: https://github.com/spddl/CS2FontStudio/stargazers "GitHub stars"

### [Download](https://github.com/spddl/CS2FontStudio/releases)

Changes the fonts of Counter-Strike 2: a font, size, weight and width of its own for every font group of the game (menus,
HUD, console, scoreboard, chat, …). A preview built from in-game screenshots shows the result before you apply it.

Looking for something simpler? [CS2 Font Configurator](https://github.com/spddl/CS2FontConfigurator) is the simple version
of this tool, without the preview.

> [!IMPORTANT]
> **This project is AI-generated.** The app's code, the preview scenes, the measuring tools and this readme were written by
> an AI, following the instructions and under the review of the project owner. Mistakes are possible:
> please report them as an issue.

> [!NOTE]
> **The preview shows Stratum2 only if it is installed on your PC.** Stratum2, the font of CS2, is a commercial font and is
> **not** shipped with this project. Without it, the preview uses free stand-in fonts that only resemble the original:
> [Saira SemiCondensed](https://fonts.google.com/specimen/Saira+Semi+Condensed),
> [Saira ExtraCondensed](https://fonts.google.com/specimen/Saira+Extra+Condensed) and
> [Chivo Mono](https://fonts.google.com/specimen/Chivo+Mono), drawn at the size and width of each Stratum2 cut, all under the
> SIL Open Font License. Spacing, line breaks and edges can therefore differ slightly from the game. The game itself always
> keeps its own fonts for everything you do not change.

## Features

- **Font groups**: CS2 requests its text through about 30 font families ("Stratum2", "Stratum2 Bold TF", "Stratum2 Mono",
  …). Each one is a group you can change on its own: font, size, weight, italic, width. All texts of a group change together.
- **Preview**: main menu, console, store, loadout, settings, watch, HUD, kill feed and scoreboard as screenshots from the
  game, with the texts drawn live in your fonts on top. Clicking a text selects its group. "Text frames" shows the fixed
  boxes where the game cuts off text that is too long.
- **Global text size**: scales every text in the game, fallback fonts included.
- **Presets**: for example "Segoe UI for all groups", "Consolas for fixed-width groups",
  "Windows 11 (Bahnschrift, Consolas)", a modern look using only fonts that ship with Windows 11, or
  "Preview fonts (Saira, Chivo Mono)", which puts the preview's stand-in fonts into the game with the same size and width,
  so the game looks like the preview without Stratum2. These fonts come with the app; it writes them to
  `%APPDATA%\CS2FontStudio\builtin` at start, nothing needs to be installed.
- **Configurations**: save, load, export as `.json` (settings only) or `.zip` (with the font files, for another PC) and
  import them again, also by drag and drop.
- **Restart CS2**: the game reads its fonts only at startup. If CS2 is running, the app offers to restart it.

## Installation

1. Download `CS2FontStudio.exe` from the [releases page](https://github.com/spddl/CS2FontStudio/releases) and put
   it into any folder. No installation needed.
2. Start it. The app opens in your default browser (it runs as a small local web server on `127.0.0.1`, not on the
   internet). If no default browser is set, the console window shows the address to open by hand. Windows may warn
   about an unknown publisher (SmartScreen), because the exe is not signed: "More info" › "Run anyway".
3. The CS2 folder is found through Steam automatically. If not, enter the path in the app (the folder that contains
   `game\csgo`).
4. Choose your fonts, click **Apply to game** and restart CS2.

To quit, close the console window (it is minimized after the start). Start options: `-no-open` (do not open the
browser), `-port 8080` (fixed port instead of a free one).

Requires Windows with Counter-Strike 2 installed. The app offers the fonts installed in Windows (system-wide or for the
current user) plus fonts from imported configurations.

### Building

Requires Go 1.26.

```sh
go test ./...
go build -ldflags="-w -s" -o CS2FontStudio.exe .
```

The web interface (`web/`) is embedded into the exe. Build it as a console program (no `-H windowsgui`): the console
window is how the app is closed.

## How it works

CS2 draws its interface (Panorama) with Pango and looks up fonts through
[fontconfig](https://www.freedesktop.org/wiki/Software/fontconfig/). Valve ships a configuration that also loads extra rules
from `game\core\panorama\fonts\conf.d\`. The app builds on that:

- **One rule file**: applying writes `game\core\panorama\fonts\conf.d\99-spddl.conf`. For each changed group it says "when
  the game asks for family *X*, use font *Y*", plus size, weight, italic and width. A copy of the settings is stored as a
  comment in the same file, so the app knows on its next start what is applied.
- **Font files**: the game only loads font files from `game\csgo\panorama\fonts` whose name matches its pattern. The chosen
  fonts are therefore copied there with the prefix `notosans-` (for example `notosans-arial.ttf`). Files that belong to the
  game are never overwritten.
- **Clean-up**: when applying and when resetting, the app removes every font file in that folder that does not belong to the
  game: old copies from earlier settings, but also fonts put there by hand or by other tools. `fonts.conf`,
  `stratum2.uifont` and all other files are left alone.
- **Cache**: the fontconfig cache (`%TEMP%\fontconfig`) is cleared so CS2 picks up the change on its next start.
- Nothing of the game itself is modified: no program files, no memory of a running game. There is no guarantee how Valve
  will treat such changes in the future.

### How the preview is made

The scenes were measured in the real game at 1920x1080 (the measuring tools are not part of this repository): CS2 was started with measuring fonts in which every
character is a bar with the original advance width. Several runs, each with one bit per group, show which text belongs to
which group. So position, size and group of every text come from the game, not from guesses. The backgrounds are screenshots
taken with invisible fonts, i.e. without any text.

Limits: the preview is rendered by the browser, the game uses Pango and FreeType without hinting. Small differences in edges,
kerning and line height are normal. Some texts use a default font in the game that no group reaches (for example the
counters in the right-hand bar of the main menu). They are shown in the preview but cannot be selected.

## Uninstalling

1. Click **Reset** in the app. This removes `conf.d\99-spddl.conf` and the copied fonts from the game folder. Restart CS2
   afterwards.
2. Delete `CS2FontStudio.exe`.
3. Optionally delete the app's data: `%APPDATA%\CS2FontStudio` (the CS2 path it found, saved configurations, fonts
   from imported bundles, the bundled stand-in fonts), and configurations stored next to the exe in the `profiles` folder.

Without the app, by hand:

- delete `…\Counter-Strike Global Offensive\game\core\panorama\fonts\conf.d\99-spddl.conf`,
- in `…\Counter-Strike Global Offensive\game\csgo\panorama\fonts`, delete the `notosans-*.ttf`/`.otf` files the app copied,
- or in Steam: Counter-Strike 2 › Properties › Installed Files › **Verify integrity of game files**.

## Project layout

| Path | Content |
|---|---|
| `*.go` | the app (Go): local server, font lookup, writing the rules, configurations |
| `web/` | interface and preview (HTML/CSS/JS, scenes, stand-in fonts) |

## Font licenses

- **Stratum2** belongs to its rights holders and is not part of this project. The preview uses it only if it is installed
  locally.
- **Saira SemiCondensed**, **Saira ExtraCondensed** (`web/fonts/OFL-Saira.txt`) and **Chivo Mono**
  (`web/fonts/OFL-ChivoMono.txt`): SIL Open Font License 1.1, unmodified static files from Google Fonts and the Chivo
  project.
- Fonts you choose in the app are copied into the game's folder. When you share a `.zip` export with fonts, their licenses
  apply.
