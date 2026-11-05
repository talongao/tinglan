package main

import "github.com/vivaui/viva/ui"

type palette struct {
	base, surface, text, secondary, muted, accent uint32
	dark                                          bool
}

func (m *model) palette() palette {
	return paletteFor(m.settings.Appearance.Theme)
}

// paletteFor resolves one named theme without touching the current settings;
// the appearance picker uses it to draw a real preview swatch.
func paletteFor(theme string) palette {
	switch theme {
	case "ocean":
		return palette{0xeaf1f5, 0xf6fbff, 0x183247, 0x446277, 0x6c8596, 0x216b9a, false}
	case "sage":
		return palette{0xeef2e9, 0xf8fbf4, 0x273a2b, 0x566854, 0x7a8a73, 0x42784e, false}
	case "sand":
		return palette{0xf3ece1, 0xfffaf1, 0x453326, 0x75634e, 0x97836c, 0xa8622d, false}
	case "rose":
		return palette{0xf4e9ed, 0xfff6f8, 0x442a35, 0x775966, 0x9b7b89, 0xb0446a, false}
	case "ice":
		return palette{0xe8f2f1, 0xf4fcfa, 0x203b3a, 0x4c706c, 0x73928e, 0x197f79, false}
	case "wine":
		return palette{0x21161c, 0x302129, 0xf7e8ef, 0xd4b8c5, 0xb18d9e, 0xee99bb, true}
	case "coffee":
		return palette{0x211c18, 0x302820, 0xf5ecdf, 0xd2bea5, 0xa68e73, 0xe7b377, true}
	case "obsidian":
		return palette{0x111314, 0x1c2022, 0xf0f3f4, 0xb9c4c8, 0x899a9f, 0xa5d0d7, true}
	case "midnight":
		return palette{0x101923, 0x1b2837, 0xe8eff7, 0xbac9dc, 0x8da1ba, 0x81b5f5, true}
	case "forest":
		return palette{0x101d19, 0x1d3028, 0xe7f1e9, 0xb5cec0, 0x8ca999, 0x81cba3, true}
	case "graphite":
		return palette{0x19191c, 0x29292e, 0xf0efed, 0xc4c2c6, 0x99969f, 0xe3ae86, true}
	default:
		return palette{0xf1f0ed, 0xffffff, 0x16181c, 0x5c6169, 0x91969e, 0xc8202e, false}
	}
}
func (m *model) ink() ui.Color              { return color(m.palette().text, 1) }
func (m *model) muted() ui.Color            { return color(m.palette().muted, 1) }
func (m *model) secondary() ui.Color        { return color(m.palette().secondary, 1) }
func (m *model) surface(a float32) ui.Color { return color(m.palette().surface, a) }
func (m *model) line(a float32) ui.Color    { return color(m.palette().text, a) }
func (m *model) controlHoverFill() ui.Color {
	base, tint := m.surface(1), m.accent()
	return ui.Color{Red: base.Red*.9 + tint.Red*.1, Green: base.Green*.9 + tint.Green*.1, Blue: base.Blue*.9 + tint.Blue*.1, Alpha: 1}
}
func (m *model) fieldTheme() ui.Theme {
	t := ui.LightTheme()
	if m.palette().dark {
		t = ui.DarkTheme()
	}
	t.Primary = m.accent()
	return t
}

var themes = tree.Sub(shell, tree.Name("themes"))
var themeNames = []string{"light", "ocean", "sage", "sand", "rose", "ice", "midnight", "forest", "graphite", "wine", "coffee", "obsidian"}
var themeLabels = []string{"晨光", "晴海", "鼠尾草", "暖砂", "蔷薇", "冰川", "午夜蓝", "深林", "石墨", "夜玫瑰", "咖啡", "曜石"}
